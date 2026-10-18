// Command holybook is the single binary of the holybook app: the web server
// and the content tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

type command struct {
	name, usage string
	run         func(ctx context.Context, args []string) error
}

var commands = []command{
	{"serve", "run the web server", serve},
	{"import", "replace the database content with a directory of book XML files", runImport},
	{"fetch-index", "download the book indices from the source websites", fetchIndex},
	{"fetch-content", "download the books listed in index files as XML", fetchContent},
	{"index", "write a JSON index of book titles and paths in a content directory", index},
	{"eval", "evaluate how verbatim a translator reproduces authoritative texts", evaluate},
}

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: holybook <command> [flags]\n\nCommands:\n")
	for _, c := range commands {
		fmt.Fprintf(os.Stderr, "  %-14s %s\n", c.name, c.usage)
	}
	fmt.Fprintf(os.Stderr, "\nRun 'holybook <command> -h' for the flags of a command.\n")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	setupLogging()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	name := os.Args[1]
	for _, c := range commands {
		if c.name == name {
			if err := c.run(ctx, os.Args[2:]); err != nil {
				if errors.Is(err, flag.ErrHelp) {
					os.Exit(2)
				}
				slog.Error(name+" failed", "err", err)
				os.Exit(1)
			}
			return
		}
	}
	if name == "-h" || name == "--help" || name == "help" {
		usage()
		return
	}
	fmt.Fprintf(os.Stderr, "unknown command %q\n\n", name)
	usage()
	os.Exit(2)
}

// setupLogging configures slog from LOG_LEVEL (debug, info, warn, error) and
// LOG_FORMAT (text, json).
func setupLogging() {
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewTextHandler(os.Stderr, opts)
	if os.Getenv("LOG_FORMAT") == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: holybook %s [flags]\n\nFlags:\n", name)
		fs.PrintDefaults()
	}
	return fs
}
