# Instructions for coding agents

Holybook serves the Bahá’í writings: a Go web server (server-rendered HTML with
htmx, plus a JSON API) over PostgreSQL, and CLI tools that fetch the texts and
import them. Everything is one binary, `cmd/holybook`. See
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

- Go 1.25, standard library first (`net/http` routing, `html/template`,
  `log/slog`, `flag`). Dependencies: pgx, goquery, genai, x/text, x/term.
- Configuration through environment variables (see README); database passwords
  only via `DB_PASSWORD` or a prompt, never in URLs or flags.
- Paragraph indexes are identical across the language versions of a book; the
  verbatim translation and the reader's language switch rely on it.

## Deployment

Docker Compose on a DigitalOcean droplet (Postgres, server, Caddy), deployed by
`.github/workflows/deploy.yml` on pushes to `main`. Files in `deploy/`; see
[deploy/README.md](deploy/README.md).

## Git protocol

**Always get explicit user approval before committing or pushing.** Present a
summary of the changes and wait for confirmation before `git commit` or
`git push`.
