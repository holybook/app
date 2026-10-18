// Package store is the PostgreSQL access layer: the schema, the read queries
// used by the web server, and the bulk import of book content.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultURL is the database used when none is configured (local development).
const DefaultURL = "postgres://127.0.0.1:5432/holybook"

// Config describes a database connection. Credentials are kept out of the URL
// so that passwords may contain any character.
type Config struct {
	URL      string
	User     string
	Password string
}

// Store wraps a connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to the database and verifies the connection.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	url := cfg.URL
	if url == "" {
		url = DefaultURL
	}
	pc, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parsing database url: %w", err)
	}
	if cfg.User != "" {
		pc.ConnConfig.User = cfg.User
	}
	if cfg.Password != "" {
		pc.ConnConfig.Password = cfg.Password
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to %s@%s/%s: %w",
			pc.ConnConfig.User, pc.ConnConfig.Host, pc.ConnConfig.Database, err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
