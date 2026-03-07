package metrics_test

import (
	"sort"
	"testing"

	"github.com/limexpress/gateway/internal/metrics"
)

func TestRequestFields_ExactSchema(t *testing.T) {
	fields := metrics.RequestFields(
		"req-1",
		"org-1",
		"user-1",
		"team-1",
		"openai",
		"gpt-4o",
		42,
		10,
		20,
		0.123,
	)

	got := make([]string, 0, len(fields))
	for _, f := range fields {
		got = append(got, f.Key)
	}
	sort.Strings(got)

	want := []string{
		"cost_usd",
		"input_tok_count",
		"latency_ms",
		"model",
		"org_id",
		"output_tok_count",
		"provider",
		"request_id",
		"team_id",
		"user_id",
	}
	sort.Strings(want)

	if len(got) != len(want) {
		t.Fatalf("field count=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("schema mismatch at %d: got %q want %q", i, got[i], want[i])
		}
	}
}
