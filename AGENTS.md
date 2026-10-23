# Instructions for coding agents

Holybook serves the Bahá’í writings: a Go web server (server-rendered HTML with
htmx, a JSON API and an MCP endpoint for AI agents) over PostgreSQL, and CLI
tools that fetch the texts and import them. Everything is one binary, `cmd/holybook`. See
[README.md](README.md) for the feature overview.

## Layout

- `cmd/holybook` – subcommands: `serve`, `import`, `fetch-index`,
  `fetch-content`, `index`, `eval`.
- `internal/book` – the content model. A book is a flat list of paragraphs;
  section nesting is encoded in `SectionPath` ("1.2") and `SectionTitle`
  paragraphs. `Builder` assigns indexes, section paths and per-section body
  numbers (label `1.2:5`). Also the book XML format (`ReadXML`/`WriteXML`, which
  must stay byte-compatible with the holybook/data repository) and index
  descriptors.
- `internal/store` – raw SQL with pgx, no ORM. `schema.go` holds the DDL;
  `Import` rebuilds everything in one transaction. Bump `SchemaVersion` on any
  schema change so deployed databases get reimported.
- `internal/fetch` – scrapers for bahai.org and bibliothek.bahai.de (goquery).
- `internal/translate` – Gemini (`google.golang.org/genai`) translators and
  `Service`, which looks up authoritative translations and rejects model output
  that alters them.
- `internal/web` – handlers (`pages.go`, `api.go`), `templates/` and `static/`
  (embedded). Pages render fully server-side; htmx requests get fragments
  (`renderPage` with a fragment template name). Keep every page usable without
  JavaScript, and keep `static/app.js` to progressive enhancements. No inline
  scripts (CSP).
- `internal/mcp` – the stateless MCP endpoint `/mcp` (official Go SDK) with the
  tools `search_writings`, `get_passage`, `list_books` and
  `find_authoritative_translation`. Queries in `store/research.go`.
- `internal/quote` – recognizes quotations: normalized excerpt and overlap
  matching, candidate retrieval through full-text search on short phrases.
- `plugins/holybook` – the agent plugin (skills + MCP config) for Claude, Codex
  and Gemini; see [plugins/holybook/README.md](plugins/holybook/README.md). Its
  skills are the single source of guidance: `plugins/skills.go` embeds them and
  the server offers them as MCP prompts. Keep the tool descriptions,
  `internal/mcp/instructions.txt` and the skills consistent.

## Commands

```bash
make run          # dev server on :8080, templates/static reloaded from disk
make import       # load ../data/content into $DATABASE_URL
make test         # needs a scratch database at $TEST_DATABASE_URL (it is wiped)
make test-live    # network: compare fetched books with ../data
make fmt          # gofmt + go vet
```

Tests that need external resources skip unless configured:
`HOLYBOOK_TEST_DATABASE_URL` (store tests), `HOLYBOOK_CONTENT` (corpus XML round
trip), `HOLYBOOK_DATA` (live fetch parity).

## Conventions

- Go 1.26, standard library first (`net/http` routing, `html/template`,
  `log/slog`, `flag`). Dependencies: pgx, goquery, genai, MCP go-sdk, x/text,
  x/term.
- Configuration through environment variables (see README); database passwords
  only via `DB_PASSWORD` or a prompt, never in URLs or flags.
- Paragraph indexes are meant to be identical across the language versions of
  a book; the verbatim translation and the reader's language switch rely on it.
  Many books in the data do not hold to it yet (translations add or drop
  headings and paragraph breaks). `find_authoritative_translation` therefore
  only maps a paragraph between languages if the versions have as many
  paragraphs and the paragraphs have the same type and number; otherwise it
  reports `paragraph_count_mismatch` and logs a warning with `corpus=true`.
  Those log lines are the work list for fixing the data in holybook/data.

## Deployment

Docker Compose on a DigitalOcean droplet (Postgres, server, Caddy), deployed by
`.github/workflows/deploy.yml` on pushes to `main`. Files in `deploy/`; see
[deploy/README.md](deploy/README.md).

## Git protocol

**Always get explicit user approval before committing or pushing.** Present a
summary of the changes and wait for confirmation before `git commit` or
`git push`.
