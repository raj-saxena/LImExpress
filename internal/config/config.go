package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Config struct {
	Server    ServerConfig
	DB        DBConfig
	Providers ProvidersConfig
	Pricing   map[string]ModelPrice
	Budgets   BudgetDefaults
	Log       LogConfig
}

type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type DBConfig struct {
	DSN string `mapstructure:"dsn"`
}

type ProvidersConfig struct {
	Anthropic AnthropicConfig
	OpenAI    OpenAIConfig
	Vertex    VertexConfig
}

type AnthropicConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type OpenAIConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type VertexConfig struct {
	ProjectID string `mapstructure:"project_id"`
	Location  string `mapstructure:"location"`
}

// ModelPrice is cost per million tokens in USD.
type ModelPrice struct {
	InputPerMToken  float64 `mapstructure:"input_per_m_token"`
	OutputPerMToken float64 `mapstructure:"output_per_m_token"`
}

type BudgetDefaults struct {
	UserDayUSD float64 `mapstructure:"user_day_usd"`
	TeamDayUSD float64 `mapstructure:"team_day_usd"`
}

type LogConfig struct {
	Level string `mapstructure:"level"`
}

func Load() (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "300s") // long for SSE streams
	v.SetDefault("log.level", "info")
	v.SetDefault("budgets.user_day_usd", 10.0)
	v.SetDefault("budgets.team_day_usd", 100.0)
	v.SetDefault("pricing", map[string]any{
		"claude-3-5-sonnet-20241022": map[string]any{"input_per_m_token": 3.0, "output_per_m_token": 15.0},
		"claude-3-haiku-20240307":   map[string]any{"input_per_m_token": 0.25, "output_per_m_token": 1.25},
		"gpt-4o":                    map[string]any{"input_per_m_token": 5.0, "output_per_m_token": 15.0},
		"gpt-4o-mini":               map[string]any{"input_per_m_token": 0.15, "output_per_m_token": 0.6},
	})

	// Env
	v.SetEnvPrefix("LIMEXPRESS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Map common provider env vars developers already export
	_ = v.BindEnv("providers.anthropic.api_key", "ANTHROPIC_API_KEY")
	_ = v.BindEnv("providers.openai.api_key", "OPENAI_API_KEY")
	_ = v.BindEnv("db.dsn", "DB_DSN")

	// Optional config file
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("/etc/limexpress")
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshalling config: %w", err)
	}
	return &cfg, nil
}

// LogSummary logs non-sensitive config fields at INFO level.
func (c *Config) LogSummary() {
	logger, _ := zap.NewProduction()
	defer logger.Sync() //nolint:errcheck

	logger.Info("config loaded",
		zap.Int("server.port", c.Server.Port),
		zap.Duration("server.read_timeout", c.Server.ReadTimeout),
		zap.Duration("server.write_timeout", c.Server.WriteTimeout),
		zap.String("log.level", c.Log.Level),
		zap.Bool("db.dsn_set", c.DB.DSN != ""),
		zap.Bool("anthropic_key_set", c.Providers.Anthropic.APIKey != ""),
		zap.Bool("openai_key_set", c.Providers.OpenAI.APIKey != ""),
		zap.String("vertex.project_id", c.Providers.Vertex.ProjectID),
		zap.String("vertex.location", c.Providers.Vertex.Location),
		zap.Int("pricing_models", len(c.Pricing)),
		zap.Float64("budgets.user_day_usd", c.Budgets.UserDayUSD),
		zap.Float64("budgets.team_day_usd", c.Budgets.TeamDayUSD),
	)
}
