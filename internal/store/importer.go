package store

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/holybook/go/internal/book"
)

// ImportStats summarizes an import.
type ImportStats struct {
	Books        int
	Translations int
	Paragraphs   int
}

// Import replaces the entire content of the database with the given books.
//
// Everything happens in a single transaction (PostgreSQL DDL is transactional):
// readers keep seeing the previous content until the new one is committed, and
// a failed import leaves the database untouched. When commit is non-empty it is
// recorded in sync_state as the applied content commit, again only on success.
func (s *Store) Import(ctx context.Context, books iter.Seq2[*book.Content, error], commit string) (ImportStats, error) {
	var stats ImportStats
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, stmt := range []string{dropSchema, createSchema, createSyncState} {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("creating schema: %w", err)
			}
		}

		authors := map[string]bool{}
		for i, a := range SeedAuthors {
			if err := insertAuthor(ctx, tx, a, i+1); err != nil {
				return err
			}
			authors[a.ID] = true
		}

		seen := map[string]bool{}
		for c, err := range books {
			if err != nil {
				return err
			}
			if !authors[c.Author] {
				slog.Warn("unknown author, using its code as display name", "author", c.Author, "book", c.ID)
				if err := insertAuthor(ctx, tx, Author{ID: c.Author, Names: map[string]string{}}, 1000+len(authors)); err != nil {
					return err
				}
				authors[c.Author] = true
			}
			if !seen[c.ID] {
				var published any
				if !c.PublishedAt.IsZero() {
					published = c.PublishedAt
				}
				if _, err := tx.Exec(ctx,
					`INSERT INTO books(id, author, published_at) VALUES ($1, $2, $3)`,
					c.ID, c.Author, published); err != nil {
					return fmt.Errorf("inserting book %s: %w", c.ID, err)
				}
				seen[c.ID] = true
				stats.Books++
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO translations(book, language, title) VALUES ($1, $2, $3)`,
				c.ID, c.Language, c.Title); err != nil {
				return fmt.Errorf("inserting book %s (%s): %w", c.ID, c.Language, err)
			}
			stats.Translations++

			n, err := tx.CopyFrom(ctx, pgx.Identifier{"paragraphs"},
				[]string{"book", "language", "index", "number", "section_path", "type", "text"},
				pgx.CopyFromSlice(len(c.Paragraphs), func(i int) ([]any, error) {
					p := c.Paragraphs[i]
					var number any
					if p.Number != nil {
						number = int32(*p.Number)
					}
					return []any{c.ID, c.Language, int32(p.Index), number, p.SectionPath, string(p.Type), p.Text}, nil
				}))
			if err != nil {
				return fmt.Errorf("inserting paragraphs of %s (%s): %w", c.ID, c.Language, err)
			}
			stats.Paragraphs += int(n)
			slog.Debug("imported", "book", c.ID, "language", c.Language, "paragraphs", n)
		}

		// Building the full-text index once after loading is much faster than
		// maintaining it row by row.
		if _, err := tx.Exec(ctx, `CREATE INDEX paragraphs_text_tokens_idx ON paragraphs USING GIN (text_tokens)`); err != nil {
			return fmt.Errorf("creating search index: %w", err)
		}
		if _, err := tx.Exec(ctx, `ANALYZE`); err != nil {
			return err
		}

		state := map[string]string{"schema_version": SchemaVersion, "applied_commit": commit}
		for key, value := range state {
			if _, err := tx.Exec(ctx, `
				INSERT INTO sync_state(key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value); err != nil {
				return fmt.Errorf("recording %s: %w", key, err)
			}
		}
		return nil
	})
	return stats, err
}

func insertAuthor(ctx context.Context, tx pgx.Tx, a Author, position int) error {
	if _, err := tx.Exec(ctx, `INSERT INTO authors(id, position) VALUES ($1, $2)`, a.ID, position); err != nil {
		return fmt.Errorf("inserting author %s: %w", a.ID, err)
	}
	for lang, name := range a.Names {
		if _, err := tx.Exec(ctx,
			`INSERT INTO author_names(author, language, name) VALUES ($1, $2, $3)`,
			a.ID, lang, name); err != nil {
			return fmt.Errorf("inserting author name %s/%s: %w", a.ID, lang, err)
		}
	}
	return nil
}

// AppliedCommit returns the content commit recorded by the last successful
// import, or "" if there is none or the database was imported with an older
// schema (so a reimport is needed either way).
func (s *Store) AppliedCommit(ctx context.Context) (string, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('sync_state') IS NOT NULL`).Scan(&exists); err != nil || !exists {
		return "", err
	}
	var commit string
	err := s.pool.QueryRow(ctx, `
		SELECT c.value FROM sync_state c JOIN sync_state v ON v.key = 'schema_version' AND v.value = $1
		WHERE c.key = 'applied_commit'`, SchemaVersion).Scan(&commit)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return commit, err
}

// SchemaUpToDate reports whether the database was imported with the current
// schema.
func (s *Store) SchemaUpToDate(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `
		SELECT to_regclass('sync_state') IS NOT NULL
		   AND to_regprocedure('search_config(text)') IS NOT NULL`).Scan(&ok)
	if err != nil || !ok {
		return false, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM sync_state WHERE key = 'schema_version' AND value = $1)`, SchemaVersion).Scan(&ok)
	return ok, err
}
