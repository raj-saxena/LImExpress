package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostChargeAccounting_UnknownModelStillRecordsUsageWithZeroCost(t *testing.T) {
	q := newMockQuerier()
	mw := PostChargeAccounting(q, map[string]config.ModelPrice{
		"known-model": {InputPerMToken: 1.0, OutputPerMToken: 1.0},
	})

	kac := makeKAC()
	inner := mw(proxySimulator("unknown-model", "openai", 321, 123))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), kac))

	inner.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return len(q.usageEvents) == 1 &&
			len(q.aggHourCalls) == 1 &&
			len(q.aggDayCalls) == 1
	}, time.Second, 10*time.Millisecond)

	q.mu.Lock()
	defer q.mu.Unlock()
	require.Len(t, q.usageEvents, 1)
	evt := q.usageEvents[0]

	assert.Equal(t, "unknown-model", evt.Model)
	assert.Equal(t, int32(321), evt.InputTokens)
	assert.Equal(t, int32(123), evt.OutputTokens)

	f, err := evt.CostUsd.Float64Value()
	require.NoError(t, err)
	assert.InDelta(t, 0.0, f.Float64, 1e-9)

	require.Len(t, q.aggHourCalls, 1)
	require.Len(t, q.aggDayCalls, 1)
}
