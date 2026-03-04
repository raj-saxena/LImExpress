package metrics_test

import (
	"strings"
	"testing"

	"github.com/limexpress/gateway/internal/metrics"
)

// TestMetricsRegistered verifies that every metric can be used without panic.
// promauto registers on package init; this test just exercises each metric.
func TestMetricsRegistered(t *testing.T) {
	t.Run("RequestsTotal", func(t *testing.T) {
		metrics.RequestsTotal.WithLabelValues("acme", "anthropic", "claude-3-5-sonnet-20241022", "2xx").Inc()
	})
	t.Run("BudgetDeniedTotal", func(t *testing.T) {
		metrics.BudgetDeniedTotal.WithLabelValues("acme", "user_day").Inc()
	})
	t.Run("InputTokensTotal", func(t *testing.T) {
		metrics.InputTokensTotal.WithLabelValues("acme", "anthropic", "claude-3-5-sonnet-20241022").Add(100)
	})
	t.Run("OutputTokensTotal", func(t *testing.T) {
		metrics.OutputTokensTotal.WithLabelValues("acme", "anthropic", "claude-3-5-sonnet-20241022").Add(50)
	})
	t.Run("CostUSDTotal", func(t *testing.T) {
		metrics.CostUSDTotal.WithLabelValues("acme", "anthropic", "claude-3-5-sonnet-20241022").Add(0.001)
	})
	t.Run("StreamDurationSeconds", func(t *testing.T) {
		metrics.StreamDurationSeconds.WithLabelValues("acme", "anthropic", "claude-3-5-sonnet-20241022").Observe(2.5)
	})
	t.Run("ActiveStreams", func(t *testing.T) {
		metrics.ActiveStreams.WithLabelValues("acme").Inc()
		metrics.ActiveStreams.WithLabelValues("acme").Dec()
	})
}

// TestLoggerNew verifies that New("info") returns a non-nil logger with no error.
func TestLoggerNew(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		level := level
		t.Run(level, func(t *testing.T) {
			logger, err := metrics.New(level)
			if err != nil {
				t.Fatalf("New(%q) returned error: %v", level, err)
			}
			if logger == nil {
				t.Fatalf("New(%q) returned nil logger", level)
			}
		})
	}

	t.Run("invalid_level", func(t *testing.T) {
		_, err := metrics.New("invalid")
		if err == nil {
			t.Fatal("expected error for invalid log level, got nil")
		}
	})
}

// sensitiveKeywords are substrings that must NOT appear in any RequestFields key.
var sensitiveKeywords = []string{"key", "secret", "token", "auth", "password"}

// TestRequestFields verifies field count and that no field key leaks sensitive names.
func TestRequestFields(t *testing.T) {
	fields := metrics.RequestFields(
		"req-123", "org-abc", "user-xyz", "team-42",
		"anthropic", "claude-3-5-sonnet-20241022",
		350, 1024, 512, 0.0042,
	)

	const wantFields = 10
	if len(fields) != wantFields {
		t.Fatalf("RequestFields returned %d fields, want %d", len(fields), wantFields)
	}

	for _, f := range fields {
		key := strings.ToLower(f.Key)
		for _, kw := range sensitiveKeywords {
			if strings.Contains(key, kw) {
				t.Errorf("field key %q contains sensitive keyword %q", f.Key, kw)
			}
		}
	}
}
