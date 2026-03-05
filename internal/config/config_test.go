package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	// Clear env vars that might interfere
	os.Unsetenv("LIMEXPRESS_DB_DSN")
	os.Unsetenv("DB_DSN")
	os.Unsetenv("LIMEXPRESS_SERVER_PORT")

	t.Run("default values", func(t *testing.T) {
		// We need to provide a DSN for validation to pass
		os.Setenv("DB_DSN", "postgres://localhost:5432/test")
		defer os.Unsetenv("DB_DSN")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, 8080, cfg.Server.Port)
		assert.Equal(t, "info", cfg.Log.Level)
		assert.Equal(t, "postgres://localhost:5432/test", cfg.DB.DSN)
	})

	t.Run("env overrides", func(t *testing.T) {
		os.Setenv("LIMEXPRESS_SERVER_PORT", "9090")
		os.Setenv("DB_DSN", "postgres://localhost:5432/env")
		defer os.Unsetenv("LIMEXPRESS_SERVER_PORT")
		defer os.Unsetenv("DB_DSN")

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, 9090, cfg.Server.Port)
		assert.Equal(t, "postgres://localhost:5432/env", cfg.DB.DSN)
	})

	t.Run("validation failure", func(t *testing.T) {
		os.Unsetenv("DB_DSN")
		os.Unsetenv("LIMEXPRESS_DB_DSN")

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
			name: "invalid port",
			cfg: Config{
				DB:     DBConfig{DSN: "dsn"},
				Server: ServerConfig{Port: 0},
			},
			wantErr: true,
			msg:     "server.port must be greater than 0",
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
