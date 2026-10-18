# Holybook

Read, browse and search the Bahá’í writings across languages, look up the
authorized translation of a passage, and translate text with AI while quoting
the authorized translations verbatim.

Holybook is a single Go binary: a server-rendered website enhanced with
[htmx](https://htmx.org), a JSON API, and the tools that fetch the texts from
their sources and load them into PostgreSQL.

```
fetch-index → fetch-content → holybook/data repo → import → PostgreSQL → serve
```

## Quick start

Requirements: Go 1.26+, PostgreSQL 14+, and the
[holybook/data](https://github.com/holybook/data) repository checked out next
to this one (`../data`).

```bash
createdb holybook
make import          # load ../data/content into the database (a few seconds)
make run             # http://localhost:8080
```

`make run` serves templates and static files straight from `internal/web`, so
edits show up on reload without restarting. Set `GEMINI_API_KEY` to enable AI
translation.

## Commands

All functionality lives in one binary, `holybook <command>`:

| Command | Purpose |
| --- | --- |
| `serve` | Run the website and JSON API. |
| `import` | Replace the database content with a directory of book XML files, in one transaction. `-commit` records the content commit; with `-if-changed` the import is skipped when that commit is already imported with the current schema. |
| `fetch-index` | Download the book listings from bahai.org and bibliothek.bahai.de into index files (`<lang> <author> <id> <url> [<date>]` per line). |
| `fetch-content` | Download every book listed in index files that is not yet in the content directory, as XML. |
| `index` | Write a JSON index of the titles and paths of a content directory. |
| `eval` | Measure how often a translator reproduces authoritative translations verbatim, over JSON test cases. |

Run `holybook <command> -h` for flags.

### Configuration

| Variable | Used by | Default |
| --- | --- | --- |
| `DATABASE_URL` | all database commands | `postgres://127.0.0.1:5432/holybook` |
| `DB_USER`, `DB_PASSWORD` | all database commands | (from the URL / none) |
| `PORT` | `serve` | `8080` |
| `GEMINI_API_KEY` | `serve`, `eval` | AI translation disabled |
| `GEMINI_MODEL` | `serve`, `eval` | `gemini-3.1-flash-lite` |
| `LOG_LEVEL`, `LOG_FORMAT` | all | `info`, `text` (or `json`) |

Database credentials are kept out of the URL and the command line, so
passwords may contain any character. `import -prompt-password` asks for the
password interactively.

## Website

| Page | |
| --- | --- |
| `/` | The library, grouped by author; long series of dated messages are grouped by year. The filter matches authors and titles while typing (ignoring accents). |
| `/books/{id}?lang=` | The reader, with a table of contents that follows the reading position. Paragraphs are addressable as `#p-<index>`; switching the language keeps the position, since paragraph indexes are the same in all languages. Legacy `?pos=<index>:<offset>` links still work. |
| `/search?q=&lang=` | Full-text search (stemmed, with `"phrases"`, `-exclusions` and `or`), best matches first, loading more results while scrolling. |
| `/translate` | Verbatim translation: finds the authorized rendering of a passage in another language as you type. |
| `/aitranslate` | AI translation that replaces quotations from the writings by their authorized translations, with citations. |

Every page works without JavaScript; htmx adds partial updates and a small
script (`static/app.js`) adds conveniences such as the reading-position
features. Light and dark themes follow the system setting.

### JSON API

The routes and response shapes of the previous Ktor server are kept:

| Route | Response |
| --- | --- |
| `GET /api/languages` | `["de", "en"]` |
| `GET /api/authors` | `[{id, names: {lang: name}}]` |
| `GET /api/books?lang=` | `{authorName: [{bookId, language, title, author}]}` |
| `GET /api/books/{id}` | `{id, paragraphCount, translations: [...]}` |
| `GET /api/books/{id}/paragraphs?lang=&start=&end=` | `[{index, text, type, number, sectionPath}]` |
| `GET /api/search?q=&lang=` | `[{bookId, author, title, highlightedText, paragraph}]` |
| `POST /api/translate` `{fromLanguage, toLanguage, text}` | `{translatedParagraph, allOriginalResults}` |
| `POST /api/aitranslate` `{fromLanguage, toLanguage, text}` | `{paragraphs: [{annotation, text}]}` |

## Development

```bash
make test        # all tests; database tests use $TEST_DATABASE_URL (it is wiped)
make test-live   # compare freshly fetched books with the data repository
make fmt
```

| Package | |
| --- | --- |
| `cmd/holybook` | Subcommands and configuration. |
| `internal/book` | Paragraph model with section hierarchy, book XML format, index descriptors. |
| `internal/store` | PostgreSQL schema, queries and the transactional importer. |
| `internal/fetch` | Source index and content scrapers. |
| `internal/translate` | Gemini translators and the verbatim-preserving translation service. |
| `internal/eval` | Translator evaluation. |
| `internal/web` | HTTP handlers, templates, static assets (embedded in the binary). |

## Deployment

A single DigitalOcean droplet runs PostgreSQL, the holybook server and Caddy
with Docker Compose. Pushes to `main` are tested, built into one image
(`ghcr.io/holybook/go`) and deployed by GitHub Actions. See
[`deploy/README.md`](deploy/README.md).
