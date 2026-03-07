package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
	"github.com/limexpress/gateway/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- computeCost tests -------------------------------------------------------

func TestComputeCost(t *testing.T) {
	pricing := map[string]config.ModelPrice{
		"claude-3-5-sonnet-20241022": {InputPerMToken: 3.0, OutputPerMToken: 15.0},
		"gpt-4o":                     {InputPerMToken: 5.0, OutputPerMToken: 15.0},
	}

	tests := []struct {
		name         string
		model        string
		inputTokens  int32
		outputTokens int32
		wantCost     float64
	}{
		{
			name:         "known model — Anthropic",
			model:        "claude-3-5-sonnet-20241022",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     18.0, // 3 + 15
		},
		{
			name:         "known model — OpenAI",
			model:        "gpt-4o",
			inputTokens:  500_000,
			outputTokens: 200_000,
			wantCost:     5.0*0.5 + 15.0*0.2, // 2.5 + 3.0 = 5.5
		},
		{
			name:         "unknown model returns 0",
			model:        "unknown-model",
			inputTokens:  1_000_000,
			outputTokens: 1_000_000,
			wantCost:     0,
		},
		{
			name:         "zero tokens",
			model:        "gpt-4o",
			inputTokens:  0,
			outputTokens: 0,
			wantCost:     0,
		},
		{
			name:         "fractional cost",
			model:        "claude-3-5-sonnet-20241022",
			inputTokens:  100,
			outputTokens: 50,
			wantCost:     3.0*100/1_000_000 + 15.0*50/1_000_000, // 0.00000300 + 0.00000075
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeCost(tt.model, tt.inputTokens, tt.outputTokens, pricing)
			assert.InDelta(t, tt.wantCost, got, 1e-9)
		})
	}
}

// -- PostChargeAccounting middleware tests ------------------------------------

// mockQuerier captures accounting calls for test assertions.
type mockQuerier struct {
	db.Querier // embed to satisfy interface; unimplemented methods panic if called

	mu           sync.Mutex
	usageEvents  []db.InsertUsageEventParams
	aggHourCalls []db.UpsertUsageAggHourParams
	aggDayCalls  []db.UpsertUsageAggDayParams

	// Signal when InsertUsageEvent is called so tests can wait for the goroutine.
	eventCh chan struct{}
}

func newMockQuerier() *mockQuerier {
	return &mockQuerier{eventCh: make(chan struct{}, 1)}
}

func (m *mockQuerier) InsertUsageEvent(_ context.Context, arg db.InsertUsageEventParams) (db.InsertUsageEventRow, error) {
	m.mu.Lock()
	m.usageEvents = append(m.usageEvents, arg)
	m.mu.Unlock()
	select {
	case m.eventCh <- struct{}{}:
	default:
	}
	return db.InsertUsageEventRow{}, nil
}

func (m *mockQuerier) UpsertUsageAggHour(_ context.Context, arg db.UpsertUsageAggHourParams) error {
	m.mu.Lock()
	m.aggHourCalls = append(m.aggHourCalls, arg)
	m.mu.Unlock()
	return nil
}

func (m *mockQuerier) UpsertUsageAggDay(_ context.Context, arg db.UpsertUsageAggDayParams) error {
	m.mu.Lock()
	m.aggDayCalls = append(m.aggDayCalls, arg)
	m.mu.Unlock()
	return nil
}

// waitForEvent waits up to 2 seconds for the accounting goroutine to fire.
func (m *mockQuerier) waitForEvent(t *testing.T) {
	t.Helper()
	select {
	case <-m.eventCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for accounting goroutine")
	}
}

// makeKAC returns a KeyAuthContext with distinct UUIDs for testing.
func makeKAC() *middleware.KeyAuthContext {
	mustUUID := func(s string) pgtype.UUID {
		var u pgtype.UUID
		_ = u.Scan(s)
		return u
	}
	return &middleware.KeyAuthContext{
		OrgID:  mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TeamID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		KeyID:  mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
	}
}

var testPricing = map[string]config.ModelPrice{
	"claude-3-5-sonnet-20241022": {InputPerMToken: 3.0, OutputPerMToken: 15.0},
}

// proxySimulator is a handler that pretends to be the proxy by filling the
// *Usage pointer placed in context by PostChargeAccounting.
func proxySimulator(model, provider string, input, output int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if uPtr, ok := proxy.UsageFromContext(r.Context()); ok {
			uPtr.Model = model
			uPtr.Provider = provider
			uPtr.InputTokens = input
			uPtr.OutputTokens = output
		}
		w.WriteHeader(http.StatusOK)
	}
}

func TestPostChargeAccounting_NoAuthContext(t *testing.T) {
	q := newMockQuerier()
	mw := PostChargeAccounting(q, testPricing)

	called := false
	inner := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	// No KeyAuthContext in context.
	inner.ServeHTTP(rec, req)

	assert.True(t, called, "next handler must be called")
	assert.Empty(t, q.usageEvents, "no accounting without auth context")
}

func TestPostChargeAccounting_ZeroTokens(t *testing.T) {
	q := newMockQuerier()
	mw := PostChargeAccounting(q, testPricing)

	kac := makeKAC()
	inner := mw(proxySimulator("claude-3-5-sonnet-20241022", "anthropic", 0, 0))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), kac))

	inner.ServeHTTP(rec, req)

	// Give goroutine time to fire (it shouldn't).
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, q.usageEvents, "zero tokens must not trigger accounting")
}

func TestPostChargeAccounting_RecordsUsage(t *testing.T) {
	q := newMockQuerier()
	mw := PostChargeAccounting(q, testPricing)

	kac := makeKAC()
	inner := mw(proxySimulator("claude-3-5-sonnet-20241022", "anthropic", 1000, 500))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), kac))

	inner.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	q.waitForEvent(t)
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return len(q.aggHourCalls) == 1 && len(q.aggDayCalls) == 1
	}, time.Second, 10*time.Millisecond)

	q.mu.Lock()
	defer q.mu.Unlock()

	require.Len(t, q.usageEvents, 1)
	evt := q.usageEvents[0]
	assert.Equal(t, kac.OrgID, evt.OrgID)
	assert.Equal(t, kac.UserID, evt.UserID)
	assert.Equal(t, kac.TeamID, evt.TeamID)
	assert.Equal(t, kac.KeyID, evt.VirtualKeyID)
	assert.Equal(t, "anthropic", evt.Provider)
	assert.Equal(t, "claude-3-5-sonnet-20241022", evt.Model)
	assert.Equal(t, int32(1000), evt.InputTokens)
	assert.Equal(t, int32(500), evt.OutputTokens)

	// Cost: (1000/1e6)*3.0 + (500/1e6)*15.0 = 0.003 + 0.0075 = 0.0105
	f, _ := evt.CostUsd.Float64Value()
	assert.InDelta(t, 0.0105, f.Float64, 1e-6)

	require.Len(t, q.aggHourCalls, 1)
	require.Len(t, q.aggDayCalls, 1)
	assert.Equal(t, int64(1000), q.aggHourCalls[0].InputTokens)
	assert.Equal(t, int64(500), q.aggHourCalls[0].OutputTokens)
}

func TestPostChargeAccounting_TwoModels(t *testing.T) {
	pricing := map[string]config.ModelPrice{
		"gpt-4o":  {InputPerMToken: 5.0, OutputPerMToken: 15.0},
		"gpt-4o-mini": {InputPerMToken: 0.15, OutputPerMToken: 0.6},
	}
	q := newMockQuerier()
	mw := PostChargeAccounting(q, pricing)

	kac := makeKAC()

	// Request 1: gpt-4o, 2M input + 1M output = 10 + 15 = $25
	inner := mw(proxySimulator("gpt-4o", "openai", 2_000_000, 1_000_000))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), kac))
	inner.ServeHTTP(rec, req)
	q.waitForEvent(t)

	// Request 2: gpt-4o-mini, 1M input + 1M output = 0.15 + 0.60 = $0.75
	q2 := newMockQuerier()
	mw2 := PostChargeAccounting(q2, pricing)
	inner2 := mw2(proxySimulator("gpt-4o-mini", "openai", 1_000_000, 1_000_000))
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req2 = req2.WithContext(middleware.WithTestContext(req2.Context(), kac))
	inner2.ServeHTTP(rec2, req2)
	q2.waitForEvent(t)

	q.mu.Lock()
	f1, _ := q.usageEvents[0].CostUsd.Float64Value()
	q.mu.Unlock()
	assert.InDelta(t, 25.0, f1.Float64, 1e-4, "gpt-4o cost")

	q2.mu.Lock()
	f2, _ := q2.usageEvents[0].CostUsd.Float64Value()
	q2.mu.Unlock()
	assert.InDelta(t, 0.75, f2.Float64, 1e-4, "gpt-4o-mini cost")
}
