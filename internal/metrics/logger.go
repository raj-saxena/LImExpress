package metrics

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a production zap logger configured for JSON output.
// level must be one of "debug", "info", "warn", "error".
func New(level string) (*zap.Logger, error) {
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", level, err)
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(zapLevel)
	// Ensure JSON encoding (production default, but be explicit).
	cfg.Encoding = "json"

	logger, err := cfg.Build()
	if err != nil {
		return nil, fmt.Errorf("building logger: %w", err)
	}
	return logger, nil
}

// RequestFields returns the standard zap fields to log per request.
// NEVER include key material, request bodies, or response bodies.
func RequestFields(
	requestID, orgID, userID, teamID, provider, model string,
	latencyMs int64,
	inputTokens, outputTokens int,
	costUSD float64,
) []zap.Field {
	return []zap.Field{
		zap.String("request_id", requestID),
		zap.String("org_id", orgID),
		zap.String("user_id", userID),
		zap.String("team_id", teamID),
		zap.String("provider", provider),
		zap.String("model", model),
		zap.Int64("latency_ms", latencyMs),
		zap.Int("input_tok_count", inputTokens),
		zap.Int("output_tok_count", outputTokens),
		zap.Float64("cost_usd", costUSD),
	}
}
