package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Mock querier for analytics tests -----------------------------------------

// analyticsMock implements db.Querier for analytics tests.
// Unimplemented methods are provided by the embedded mockQuerier.
type analyticsMock struct {
	db.Querier // embed to satisfy interface; unimplemented methods panic if called

	dailyRows      []db.GetDailyUsageByOrgRow
	dailyErr       error
	dailyParams    *db.GetDailyUsageByOrgParams

	topUserRows    []db.GetTopUsersByOrgRow
	topUserErr     error
	topUserParams  *db.GetTopUsersByOrgParams

	topModelRows   []db.GetTopModelsByOrgRow
	topModelErr    error
	topModelParams *db.GetTopModelsByOrgParams
}

func (m *analyticsMock) GetDailyUsageByOrg(_ context.Context, arg db.GetDailyUsageByOrgParams) ([]db.GetDailyUsageByOrgRow, error) {
	m.dailyParams = &arg
	return m.dailyRows, m.dailyErr
}

func (m *analyticsMock) GetTopUsersByOrg(_ context.Context, arg db.GetTopUsersByOrgParams) ([]db.GetTopUsersByOrgRow, error) {
	m.topUserParams = &arg
	return m.topUserRows, m.topUserErr
}

func (m *analyticsMock) GetTopModelsByOrg(_ context.Context, arg db.GetTopModelsByOrgParams) ([]db.GetTopModelsByOrgRow, error) {
	m.topModelParams = &arg
	return m.topModelRows, m.topModelErr
}

// -- Helpers ------------------------------------------------------------------

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	_ = u.Scan(s)
	return u
}

func makeTestKAC() *middleware.KeyAuthContext {
	return &middleware.KeyAuthContext{
		OrgID:  mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID: mustUUID("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		TeamID: mustUUID("cccccccc-cccc-cccc-cccc-cccccccccccc"),
		KeyID:  mustUUID("dddddddd-dddd-dddd-dddd-dddddddddddd"),
	}
}

func mustNumeric(f float64) pgtype.Numeric {
	return numericFromFloat(f)
}

func mustDate(s string) pgtype.Date {
	t, _ := time.Parse("2006-01-02", s)
	return pgtype.Date{Time: t, Valid: true}
}

// newTestRouter creates a chi router with the analytics handler registered.
func newTestRouter(mock *analyticsMock) http.Handler {
	r := chi.NewRouter()
	h := NewAnalyticsHandler(mock)
	h.RegisterRoutes(r)
	return r
}

// doRequest performs a test request on the given handler.
func doRequest(handler http.Handler, method, path string, kac *middleware.KeyAuthContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if kac != nil {
		req = req.WithContext(middleware.WithTestContext(req.Context(), kac))
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// -- Daily usage tests --------------------------------------------------------

func TestDailyUsage_NoAuthContext(t *testing.T) {
	mock := &analyticsMock{}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/daily", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "unauthorized", body["error"])
}

func TestDailyUsage_DefaultDateRange(t *testing.T) {
	mock := &analyticsMock{
		dailyRows: []db.GetDailyUsageByOrgRow{
			{
				Day:          mustDate("2026-03-01"),
				InputTokens:  12345,
				OutputTokens: 6789,
				CostUsd:      mustNumeric(0.15),
				RequestCount: 42,
			},
		},
	}

	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/daily", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)

	// The default window should be ~90 days ago → tomorrow.
	// Verify the window is approximately correct (within a minute of test execution).
	now := time.Now().UTC()
	expectedFrom := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -90)
	expectedTo := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

	gotFrom := mock.dailyParams.WindowStart.Time.UTC()
	gotTo := mock.dailyParams.WindowStart_2.Time.UTC()

	assert.WithinDuration(t, expectedFrom, gotFrom, time.Minute)
	assert.WithinDuration(t, expectedTo, gotTo, time.Minute)

	var body struct {
		Data []struct {
			Day          string  `json:"day"`
			InputTokens  int64   `json:"input_tokens"`
			OutputTokens int64   `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
			RequestCount int32   `json:"request_count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "2026-03-01", body.Data[0].Day)
	assert.Equal(t, int64(12345), body.Data[0].InputTokens)
	assert.Equal(t, int64(6789), body.Data[0].OutputTokens)
	assert.InDelta(t, 0.15, body.Data[0].CostUSD, 1e-6)
	assert.Equal(t, int32(42), body.Data[0].RequestCount)
}

func TestDailyUsage_CustomDateRange(t *testing.T) {
	mock := &analyticsMock{dailyRows: []db.GetDailyUsageByOrgRow{}}
	r := newTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/daily?from=2026-01-01&to=2026-02-01", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)

	wantFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, wantFrom, mock.dailyParams.WindowStart.Time.UTC())
	assert.Equal(t, wantTo, mock.dailyParams.WindowStart_2.Time.UTC())
}

func TestDailyUsage_RFC3339DateRange(t *testing.T) {
	mock := &analyticsMock{dailyRows: []db.GetDailyUsageByOrgRow{}}
	r := newTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/daily?from=2026-01-15T00:00:00Z&to=2026-02-15T00:00:00Z", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)
	assert.Equal(t, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC), mock.dailyParams.WindowStart.Time.UTC())
	assert.Equal(t, time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC), mock.dailyParams.WindowStart_2.Time.UTC())
}

func TestDailyUsage_BadFromParam(t *testing.T) {
	mock := &analyticsMock{}
	r := newTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/daily?from=not-a-date", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid date: from", body["error"])
}

func TestDailyUsage_BadToParam(t *testing.T) {
	mock := &analyticsMock{}
	r := newTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/daily?to=not-a-date", nil)
	req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "invalid date: to", body["error"])
}

func TestDailyUsage_DBError(t *testing.T) {
	mock := &analyticsMock{dailyErr: errors.New("db connection lost")}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/daily", makeTestKAC())

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "internal error", body["error"])
}

func TestDailyUsage_EmptyResult(t *testing.T) {
	mock := &analyticsMock{dailyRows: []db.GetDailyUsageByOrgRow{}}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/daily", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	// data must be [] not null
	assert.Equal(t, json.RawMessage("[]"), body["data"])
}

// -- Top users tests ----------------------------------------------------------

func TestTopUsers_NoAuthContext(t *testing.T) {
	mock := &analyticsMock{}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-users", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTopUsers_DefaultLimit(t *testing.T) {
	mock := &analyticsMock{
		topUserRows: []db.GetTopUsersByOrgRow{
			{
				ID:            mustUUID("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"),
				Email:         "user@example.com",
				TotalCostUsd:  mustNumeric(1.23),
				TotalRequests: 99,
			},
		},
	}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-users", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.topUserParams)
	assert.Equal(t, int32(10), mock.topUserParams.Limit) // default limit

	var body struct {
		Data []struct {
			UserID        string  `json:"user_id"`
			Email         string  `json:"email"`
			TotalCostUSD  float64 `json:"total_cost_usd"`
			TotalRequests int32   `json:"total_requests"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee", body.Data[0].UserID)
	assert.Equal(t, "user@example.com", body.Data[0].Email)
	assert.InDelta(t, 1.23, body.Data[0].TotalCostUSD, 1e-6)
	assert.Equal(t, int32(99), body.Data[0].TotalRequests)
}

func TestTopUsers_LimitClamping(t *testing.T) {
	tests := []struct {
		name      string
		limitParam string
		wantLimit  int32
	}{
		{"zero clamps to 1", "0", 1},
		{"negative clamps to 1", "-5", 1},
		{"over max clamps to 100", "200", 100},
		{"at max is 100", "100", 100},
		{"normal value", "25", 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &analyticsMock{topUserRows: []db.GetTopUsersByOrgRow{}}
			r := newTestRouter(mock)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/top-users?limit="+tt.limitParam, nil)
			req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			require.NotNil(t, mock.topUserParams)
			assert.Equal(t, tt.wantLimit, mock.topUserParams.Limit)
		})
	}
}

func TestTopUsers_DBError(t *testing.T) {
	mock := &analyticsMock{topUserErr: errors.New("db error")}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-users", makeTestKAC())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTopUsers_EmptyResult(t *testing.T) {
	mock := &analyticsMock{topUserRows: []db.GetTopUsersByOrgRow{}}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-users", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, json.RawMessage("[]"), body["data"])
}

// -- Top models tests ---------------------------------------------------------

func TestTopModels_NoAuthContext(t *testing.T) {
	mock := &analyticsMock{}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-models", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTopModels_DefaultLimit(t *testing.T) {
	mock := &analyticsMock{
		topModelRows: []db.GetTopModelsByOrgRow{
			{
				Model:         "gpt-4o",
				Provider:      "openai",
				TotalCostUsd:  mustNumeric(5.00),
				TotalRequests: 200,
			},
		},
	}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-models", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.topModelParams)
	assert.Equal(t, int32(10), mock.topModelParams.Limit) // default limit

	var body struct {
		Data []struct {
			Model         string  `json:"model"`
			Provider      string  `json:"provider"`
			TotalCostUSD  float64 `json:"total_cost_usd"`
			TotalRequests int32   `json:"total_requests"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Data, 1)
	assert.Equal(t, "gpt-4o", body.Data[0].Model)
	assert.Equal(t, "openai", body.Data[0].Provider)
	assert.InDelta(t, 5.00, body.Data[0].TotalCostUSD, 1e-6)
	assert.Equal(t, int32(200), body.Data[0].TotalRequests)
}

func TestTopModels_LimitClamping(t *testing.T) {
	tests := []struct {
		name       string
		limitParam string
		wantLimit  int32
	}{
		{"zero clamps to 1", "0", 1},
		{"over max clamps to 100", "200", 100},
		{"normal value", "50", 50},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &analyticsMock{topModelRows: []db.GetTopModelsByOrgRow{}}
			r := newTestRouter(mock)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/usage/top-models?limit="+tt.limitParam, nil)
			req = req.WithContext(middleware.WithTestContext(req.Context(), makeTestKAC()))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
			require.NotNil(t, mock.topModelParams)
			assert.Equal(t, tt.wantLimit, mock.topModelParams.Limit)
		})
	}
}

func TestTopModels_DBError(t *testing.T) {
	mock := &analyticsMock{topModelErr: errors.New("db error")}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-models", makeTestKAC())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestTopModels_EmptyResult(t *testing.T) {
	mock := &analyticsMock{topModelRows: []db.GetTopModelsByOrgRow{}}
	r := newTestRouter(mock)
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/top-models", makeTestKAC())

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, json.RawMessage("[]"), body["data"])
}

// -- OrgID scoping test -------------------------------------------------------

func TestDailyUsage_OrgScopedFromContext(t *testing.T) {
	mock := &analyticsMock{dailyRows: []db.GetDailyUsageByOrgRow{}}
	r := newTestRouter(mock)

	kac := &middleware.KeyAuthContext{
		OrgID: mustUUID("12345678-1234-1234-1234-123456789012"),
	}
	rec := doRequest(r, http.MethodGet, "/api/v1/usage/daily", kac)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)
	// The org ID must come from the auth context, not any query param.
	assert.Equal(t, kac.OrgID, mock.dailyParams.OrgID)
}
