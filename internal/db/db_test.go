package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv("INTEGRATION_TESTS") != "1" {
		t.Skip("skipping integration test because INTEGRATION_TESTS != 1")
	}

	ctx := context.Background()

	// Find migrations path
	// Assuming we are in internal/db, migrations are in ../../db/migrations
	wd, err := os.Getwd()
	require.NoError(t, err)
	migrationsPath := filepath.Join(wd, "..", "..", "db", "migrations")

	// Start Postgres container
	dbName := "limexpress_test"
	dbUser := "user"
	dbPassword := "password"

	postgresContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)
	defer func() {
		err := postgresContainer.Terminate(ctx)
		require.NoError(t, err)
	}()

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	t.Run("MigrateUp and Pool", func(t *testing.T) {
		// Run migrations
		err = MigrateUp(connStr, migrationsPath)
		require.NoError(t, err)

		// Test Pool connection
		pool, err := NewPool(ctx, connStr)
		require.NoError(t, err)
		defer pool.Close()

		// Verify we can query the migrated tables
		var exists bool
		err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'orgs')").Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists)

		err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'virtual_keys')").Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("MigrateDown", func(t *testing.T) {
		err = MigrateDown(connStr, migrationsPath)
		require.NoError(t, err)

		// Verify tables are gone (or at least one of them)
		pool, err := NewPool(ctx, connStr)
		require.NoError(t, err)
		defer pool.Close()

		var exists bool
		err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'orgs')").Scan(&exists)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}
