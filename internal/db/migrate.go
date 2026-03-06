package db

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrateUp runs all migrations to the latest version.
func MigrateUp(dsn string, migrationsPath string) error {
	sourceURL, err := fileSourceURL(migrationsPath)
	if err != nil {
		return err
	}
	m, err := migrate.New(sourceURL, formatDSN(dsn))
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running up migrations: %w", err)
	}
	return nil
}

// MigrateDown rolls back all migrations.
func MigrateDown(dsn string, migrationsPath string) error {
	sourceURL, err := fileSourceURL(migrationsPath)
	if err != nil {
		return err
	}
	m, err := migrate.New(sourceURL, formatDSN(dsn))
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running down migrations: %w", err)
	}
	return nil
}

// fileSourceURL builds a portable file:// URL from a migrations directory path.
func fileSourceURL(migrationsPath string) (string, error) {
	abs, err := filepath.Abs(migrationsPath)
	if err != nil {
		return "", fmt.Errorf("resolving migrations path: %w", err)
	}
	// filepath.ToSlash ensures forward slashes on all platforms.
	u := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String(), nil
}

// formatDSN converts a postgres DSN to the pgx5:// scheme expected by golang-migrate.
func formatDSN(dsn string) string {
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(dsn, prefix) {
			return "pgx5://" + strings.TrimPrefix(dsn, prefix)
		}
	}
	if !strings.HasPrefix(dsn, "pgx5://") {
		return "pgx5://" + dsn
	}
	return dsn
}
