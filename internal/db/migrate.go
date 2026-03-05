package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrateUp runs all migrations to the latest version.
func MigrateUp(dsn string, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, formatDSN(dsn))
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
	m, err := migrate.New("file://"+migrationsPath, formatDSN(dsn))
	if err != nil {
		return fmt.Errorf("creating migrate instance: %w", err)
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running down migrations: %w", err)
	}
	return nil
}

func formatDSN(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") {
		return "pgx5://" + strings.TrimPrefix(dsn, "postgres://")
	}
	if !strings.HasPrefix(dsn, "pgx5://") {
		return "pgx5://" + dsn
	}
	return dsn
}
