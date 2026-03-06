package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	// Ensure a clean baseline for all subtests; t.Setenv restores on cleanup.
	t.Setenv("LIMEXPRESS_DB_DSN", "")
	t.Setenv("DB_DSN", "")
	t.Setenv("LIMEXPRESS_SERVER_PORT", "")

	t.Run("default values", func(t *testing.T) {
		t.Setenv("DB_DSN", "postgres://localhost:5432/test")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, "info", cfg.Log.Level)
		assert.Equal(t, "postgres://localhost:5432/test", cfg.DB.DSN)
	})

	t.Run("env overrides", func(t *testing.T) {
		t.Setenv("LIMEXPRESS_SERVER_PORT", "9090")
		t.Setenv("DB_DSN", "postgres://localhost:5432/env")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, 9090, cfg.Server.Port)
		assert.Equal(t, "postgres://localhost:5432/env", cfg.DB.DSN)
	})

	t.Run("validation failure", func(t *testing.T) {
		t.Setenv("DB_DSN", "")
		t.Setenv("LIMEXPRESS_DB_DSN", "")

		cfg, err := Load()
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "db.dsn is required")
	})
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		msg     string
	}{
		{
			name: "valid config",
			cfg: Config{
				DB:     DBConfig{DSN: "dsn"},
				Server: ServerConfig{Port: 8080},
			},
			wantErr: false,
		},
		{
			name: "missing dsn",
			cfg: Config{
				Server: ServerConfig{Port: 8080},
			},
			wantErr: true,
			msg:     "db.dsn is required",
		},
		{
			name: "invalid port zero",
			cfg: Config{
				DB:     DBConfig{DSN: "dsn"},
				Server: ServerConfig{Port: 0},
			},
			wantErr: true,
			msg:     "server.port must be between 1 and 65535",
		},
		{
			name: "invalid port negative",
			cfg: Config{
				DB:     DBConfig{DSN: "dsn"},
				Server: ServerConfig{Port: -1},
			},
			wantErr: true,
			msg:     "server.port must be between 1 and 65535",
		},
		{
			name: "invalid port too large",
			cfg: Config{
				DB:     DBConfig{DSN: "dsn"},
				Server: ServerConfig{Port: 65536},
			},
			wantErr: true,
			msg:     "server.port must be between 1 and 65535",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.msg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_NewLogger(t *testing.T) {
	cfg := &Config{
		Log: LogConfig{Level: "debug"},
	}
	logger, err := cfg.NewLogger()
	require.NoError(t, err)
	assert.NotNil(t, logger)

	cfg.Log.Level = "invalid"
	logger, err = cfg.NewLogger()
	require.NoError(t, err) // zap falls back to default if level is invalid
	assert.NotNil(t, logger)
}

func TestConfig_LogSummary(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Port: 8080},
		DB:     DBConfig{DSN: "postgres://localhost:5432/test"},
	}
	logger, err := cfg.NewLogger()
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		cfg.LogSummary(logger)
	})
}
