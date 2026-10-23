// Package mcp serves the Model Context Protocol endpoint (/mcp), giving AI
// agents research and translation tools over the authoritative writings.
//
// The endpoint is stateless (every JSON-RPC request is answered on its own),
// so it needs no session affinity. It is public and read-only, like the
// website, so it requires no authentication.
package mcp

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/holybook/go/internal/book"
	"github.com/holybook/go/internal/quote"
	"github.com/holybook/go/internal/store"
	"github.com/holybook/go/plugins"
)

//go:embed instructions.txt
var instructions string

// Store is the data access the tools need (implemented by *store.Store).
type Store interface {
	quote.Candidates
	Languages(ctx context.Context) ([]string, error)
	Authors(ctx context.Context) ([]store.Author, error)
	Library(ctx context.Context, language string) ([]store.Translation, error)
	Book(ctx context.Context, id string) (*store.Book, error)
	Paragraphs(ctx context.Context, bookID, language string, start, end *int) ([]book.Paragraph, error)
	FindParagraphs(ctx context.Context, q store.ParagraphQuery) (store.ParagraphPage, error)
	ParagraphCounts(ctx context.Context, bookIDs []string) (map[string]map[string]int, error)
	ParagraphAt(ctx context.Context, bookID, language string, index int) (store.ParagraphInBook, error)
}

// Handler is the HTTP handler of the MCP endpoint.
type Handler struct {
	store Store
	links links
	http  http.Handler

	mu      sync.Mutex
	server  *mcp.Server
	builtAt time.Time
}

// catalogMaxAge is how long the languages and authors offered in the tool
// schemas are cached; an import can add some.
const catalogMaxAge = 10 * time.Minute

// NewHandler creates the MCP handler. Links in tool results point to the
// website at baseURL (e.g. https://holybook.app).
func NewHandler(st Store, baseURL string) *Handler {
	h := &Handler{store: st, links: links{strings.TrimSuffix(baseURL, "/")}}
	h.http = mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		return r.Context().Value(serverKey{}).(*mcp.Server)
	}, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// The endpoint is public; requests arrive through the reverse proxy.
		DisableLocalhostProtection: true,
	})
	return h
}

type serverKey struct{}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s, err := h.currentServer(r.Context())
	if err != nil {
		if r.Context().Err() == nil {
			slog.Error("mcp: loading the catalog", "err", err)
		}
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}
	h.http.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), serverKey{}, s)))
}

// currentServer returns the MCP server, rebuilding it when its catalog is
// outdated. If that fails, the previous server is kept in use.
func (h *Handler) currentServer(ctx context.Context) (*mcp.Server, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.server != nil && time.Since(h.builtAt) < catalogMaxAge {
		return h.server, nil
	}
	cat, err := loadCatalog(ctx, h.store)
	if err != nil {
		if h.server != nil {
			slog.Warn("mcp: refreshing the catalog", "err", err)
			return h.server, nil
		}
		return nil, err
	}
	h.server, h.builtAt = h.newServer(cat), time.Now()
	return h.server, nil
}

func (h *Handler) newServer(cat catalog) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:       "holybook",
		Title:      "Holybook – Bahá’í Writings",
		Version:    "1.0.0",
		WebsiteURL: "https://holybook.app",
	}, &mcp.ServerOptions{Instructions: instructions})
	h.addResearchTools(s, cat)
	h.addTranslationTool(s, cat)
	addSkillPrompts(s)
	return s
}

// catalog holds the languages and authors the tools accept.
type catalog struct {
	languages []string
	authors   []store.Author
}

func loadCatalog(ctx context.Context, st Store) (catalog, error) {
	var c catalog
	var err error
	if c.languages, err = st.Languages(ctx); err != nil {
		return c, err
	}
	c.authors, err = st.Authors(ctx)
	return c, err
}

func (c catalog) authorIDs() []string {
	ids := make([]string, len(c.authors))
	for i, a := range c.authors {
		ids[i] = a.ID
	}
	return ids
}

// authorDescription lists the authors as e.g. "bahaullah (Bahá’u’lláh), uhj
// (The Universal House of Justice)".
func (c catalog) authorDescription() string {
	parts := make([]string, len(c.authors))
	for i, a := range c.authors {
		name := a.Names["en"]
		if name == "" {
			name = a.ID
		}
		parts[i] = a.ID + " (" + name + ")"
	}
	return strings.Join(parts, ", ")
}

// toolFunc handles a tool call. It returns the result to encode as JSON, or an
// error; an *argumentError is reported to the agent, any other error is
// logged and reported as an internal error.
type toolFunc func(ctx context.Context, args *arguments) (any, error)

func addReadOnlyTool(s *mcp.Server, t *mcp.Tool, f toolFunc) {
	t.Annotations = &mcp.ToolAnnotations{
		Title:           t.Title,
		ReadOnlyHint:    true,
		DestructiveHint: ptr(false),
		IdempotentHint:  true,
		OpenWorldHint:   ptr(false),
	}
	s.AddTool(t, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		data, err := callTool(ctx, f, req.Params.Arguments)
		slog.Info("mcp tool call", "tool", t.Name, "duration", time.Since(start).Round(time.Microsecond), "err", err)
		var argErr *argumentError
		switch {
		case errors.As(err, &argErr):
			return errorResult(argErr.Error()), nil
		case err != nil:
			if ctx.Err() == nil {
				slog.Error("mcp tool failed", "tool", t.Name, "args", string(req.Params.Arguments), "err", err)
			}
			return errorResult("The tool failed with an internal error. Please try again later."), nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	})
}

func callTool(ctx context.Context, f toolFunc, raw json.RawMessage) ([]byte, error) {
	args := parseArguments(raw)
	if err := args.Err(); err != nil {
		return nil, err
	}
	result, err := f(ctx, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}, IsError: true}
}

// addSkillPrompts offers the agent skills as prompts, for clients that support
// MCP prompts but not skills.
func addSkillPrompts(s *mcp.Server) {
	s.AddPrompt(&mcp.Prompt{
		Name:        "research_bahai_writings",
		Description: "Research a question in the authoritative Bahá’í writings, with citations.",
		Arguments: []*mcp.PromptArgument{
			{Name: "question", Description: "The question to research", Required: true},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return skillPrompt("bahai-writings-research",
			"Research this question:\n\n"+req.Params.Arguments["question"])
	})
	s.AddPrompt(&mcp.Prompt{
		Name:        "translate_with_authoritative_quotes",
		Description: "Translate a text, using the authoritative translation for every quotation of the writings.",
		Arguments: []*mcp.PromptArgument{
			{Name: "text", Description: "The text to translate", Required: true},
			{Name: "to_language", Description: "Target language code", Required: true},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		args := req.Params.Arguments
		return skillPrompt("bahai-quote-translation",
			"Translate the following text into '"+args["to_language"]+"':\n\n"+args["text"])
	})
}

func skillPrompt(skill, task string) (*mcp.GetPromptResult, error) {
	body, err := plugins.SkillBody(skill)
	if err != nil {
		return nil, err
	}
	return &mcp.GetPromptResult{
		Description: "Instructions from the " + skill + " skill",
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: body + "\n\n---\n\n" + task},
		}},
	}, nil
}
