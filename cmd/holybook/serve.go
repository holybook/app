package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"golang.org/x/term"

	"github.com/holybook/go/internal/mcp"
	"github.com/holybook/go/internal/store"
	"github.com/holybook/go/internal/translate"
	"github.com/holybook/go/internal/web"
)

// dbOptions are the database connection flags shared by the commands.
type dbOptions struct {
	url, user      string
	promptPassword bool
}

func addDBFlags(fs *flag.FlagSet, defaultUser string) *dbOptions {
	o := &dbOptions{}
	fs.StringVar(&o.url, "db", env("DATABASE_URL", store.DefaultURL), "PostgreSQL URL, preferably without credentials [$DATABASE_URL]")
	fs.StringVar(&o.user, "user", env("DB_USER", defaultUser), "database user [$DB_USER]")
	fs.BoolVar(&o.promptPassword, "prompt-password", false, "prompt for the database password (default: $DB_PASSWORD)")
	return o
}

// open connects to the database. The password comes from $DB_PASSWORD or an
// interactive prompt, never from the command line or the URL, so it may
// contain any character and does not leak into process listings.
func (o *dbOptions) open(ctx context.Context) (*store.Store, error) {
	password := os.Getenv("DB_PASSWORD")
	if o.promptPassword {
		fmt.Fprint(os.Stderr, "Password: ")
		p, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return nil, err
		}
		password = string(p)
	}
	return store.Open(ctx, store.Config{URL: o.url, User: o.user, Password: password})
}

func serve(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	addr := fs.String("addr", ":"+env("PORT", "8080"), "listen address [$PORT]")
	dev := fs.String("dev", "", "read templates and static files from this directory (e.g. internal/web) on every request")
	model := fs.String("model", env("GEMINI_MODEL", translate.DefaultModel), "Gemini model for AI translation [$GEMINI_MODEL]")
	publicURL := fs.String("public-url", env("PUBLIC_BASE_URL", "https://holybook.app"), "public origin of the website, for links in MCP tool results [$PUBLIC_BASE_URL]")
	db := addDBFlags(fs, "")
	if err := fs.Parse(args); err != nil {
		return err
	}

	st, err := db.open(ctx)
	if err != nil {
		return err
	}
	defer st.Close()
	if ok, err := st.SchemaUpToDate(ctx); err != nil || !ok {
		slog.Warn("the database has no content or an outdated schema; run 'holybook import'", "err", err)
	}

	opts := web.Options{Store: st, DevDir: *dev, MCP: mcp.NewHandler(st, *publicURL)}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		g, err := translate.NewGemini(ctx, key, *model)
		if err != nil {
			return err
		}
		opts.Translator = &translate.Service{Finder: st, Translator: translate.Monolithic{Gemini: g}}
	} else {
		slog.Warn("GEMINI_API_KEY is not set; AI translation is disabled")
	}

	handler, err := web.New(opts)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           web.Logging(handler),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute, // AI translations can be slow
		IdleTimeout:       2 * time.Minute,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", *addr, "dev", *dev != "")
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
