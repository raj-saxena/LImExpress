package portal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/portal/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dashboardMock struct {
	db.Querier

	dailyRows   []db.GetDailyUsageByOrgRow
	dailyErr    error
	dailyParams *db.GetDailyUsageByOrgParams

	topUserRows   []db.GetTopUsersByOrgRow
	topUserErr    error
	topUserParams *db.GetTopUsersByOrgParams

	topModelRows   []db.GetTopModelsByOrgRow
	topModelErr    error
	topModelParams *db.GetTopModelsByOrgParams
}

func (m *dashboardMock) GetDailyUsageByOrg(_ context.Context, arg db.GetDailyUsageByOrgParams) ([]db.GetDailyUsageByOrgRow, error) {
	m.dailyParams = &arg
	return m.dailyRows, m.dailyErr
}

func (m *dashboardMock) GetTopUsersByOrg(_ context.Context, arg db.GetTopUsersByOrgParams) ([]db.GetTopUsersByOrgRow, error) {
	m.topUserParams = &arg
	return m.topUserRows, m.topUserErr
}

func (m *dashboardMock) GetTopModelsByOrg(_ context.Context, arg db.GetTopModelsByOrgParams) ([]db.GetTopModelsByOrgRow, error) {
	m.topModelParams = &arg
	return m.topModelRows, m.topModelErr
}

func TestDashboardData_Daily_UsesOrgContext(t *testing.T) {
	mock := &dashboardMock{dailyRows: []db.GetDailyUsageByOrgRow{{
		Day:          mustDate("2026-03-05"),
		InputTokens:  10,
		OutputTokens: 20,
		CostUsd:      mustNumeric(0.1),
		RequestCount: 2,
	}}}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/daily", nil)
	req = withDashboardCtx(req, "99999999-9999-9999-9999-999999999999")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)
	assert.Equal(t, mustUUID("99999999-9999-9999-9999-999999999999"), mock.dailyParams.OrgID)
}

func TestDashboardData_Daily_DefaultRange90Days(t *testing.T) {
	mock := &dashboardMock{dailyRows: []db.GetDailyUsageByOrgRow{}}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/daily", nil)
	req = withDashboardCtx(req, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.dailyParams)
	gotFrom := mock.dailyParams.WindowStart.Time.UTC()
	gotTo := mock.dailyParams.WindowStart_2.Time.UTC()
	now := time.Now().UTC()
	wantFrom := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -90)
	wantTo := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	assert.WithinDuration(t, wantFrom, gotFrom, time.Minute)
	assert.WithinDuration(t, wantTo, gotTo, time.Minute)
}

func TestDashboardData_TopUsers_Limit(t *testing.T) {
	mock := &dashboardMock{topUserRows: []db.GetTopUsersByOrgRow{}}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/top-users?limit=200", nil)
	req = withDashboardCtx(req, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, mock.topUserParams)
	assert.Equal(t, int32(100), mock.topUserParams.Limit)
}

func TestDashboardData_TopModels_DBError(t *testing.T) {
	mock := &dashboardMock{topModelErr: errors.New("db down")}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/top-models", nil)
	req = withDashboardCtx(req, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDashboardData_RequiresPortalContext(t *testing.T) {
	mock := &dashboardMock{}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/daily", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func withDashboardCtx(req *http.Request, orgID string) *http.Request {
	ctx := auth.ContextWithUser(req.Context(), &auth.UserContext{
		UserID: mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		Email:  "user@example.com",
	})
	ctx = auth.ContextWithOrg(ctx, &auth.OrgContext{
		OrgID: mustUUID(orgID),
		Role:  "member",
		Name:  "Acme",
	})
	return req.WithContext(ctx)
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	err := u.Scan(s)
	if err != nil {
		panic(err)
	}
	return u
}

func mustDate(s string) pgtype.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return pgtype.Date{Time: t, Valid: true}
}

func mustNumeric(f float64) pgtype.Numeric {
	return numericFromFloat(f)
}

func numericFromFloat(f float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(fmt.Sprintf("%.8f", f))
	return n
}

func TestDashboardData_Daily_ResponseShape(t *testing.T) {
	mock := &dashboardMock{dailyRows: []db.GetDailyUsageByOrgRow{{
		Day:          mustDate("2026-03-01"),
		InputTokens:  123,
		OutputTokens: 456,
		CostUsd:      mustNumeric(1.23),
		RequestCount: 7,
	}}}
	r := chi.NewRouter()
	NewDashboardDataHandler(mock).RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/portal/usage/daily", nil)
	req = withDashboardCtx(req, "11111111-1111-1111-1111-111111111111")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
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
	assert.Equal(t, int64(123), body.Data[0].InputTokens)
	assert.Equal(t, int64(456), body.Data[0].OutputTokens)
	assert.InDelta(t, 1.23, body.Data[0].CostUSD, 1e-6)
	assert.Equal(t, int32(7), body.Data[0].RequestCount)
}
