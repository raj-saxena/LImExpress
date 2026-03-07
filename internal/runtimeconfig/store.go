package runtimeconfig

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	KeyOIDCClientID     = "oidc_client_id"
	KeyOIDCClientSecret = "oidc_client_secret"
	KeyOIDCRedirectURL  = "oidc_redirect_url"
	KeySessionSecret    = "session_secret"
)

// Store persists runtime settings used to bootstrap portal auth.
type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get returns (value, true, nil) when a setting exists, (empty, false, nil)
// when it does not, and (empty, false, err) on DB errors.
func (s *Store) Get(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM runtime_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) || isUndefinedTable(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetMany upserts all provided key/value pairs atomically.
func (s *Store) SetMany(ctx context.Context, values map[string]string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for k, v := range values {
		_, err = tx.Exec(ctx, `
			INSERT INTO runtime_settings (key, value, updated_at)
			VALUES ($1, $2, now())
			ON CONFLICT (key) DO UPDATE
			SET value = EXCLUDED.value, updated_at = now()
		`, k, v)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "42P01"
	}
	return false
}
