package portal

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/portal/auth"
)

// usageMock implements db.Querier for usage page tests.
type usageMock struct {
	db.Querier

	dailyRows []db.GetDailyUsageByOrgRow
	dailyErr  error
	userRows  []db.GetTopUsersByOrgRow
	userErr   error
	modelRows []db.GetTopModelsByOrgRow
	modelErr  error
}

func (m *usageMock) GetDailyUsageByOrg(_ context.Context, _ db.GetDailyUsageByOrgParams) ([]db.GetDailyUsageByOrgRow, error) {
	return m.dailyRows, m.dailyErr
}

func (m *usageMock) GetTopUsersByOrg(_ context.Context, _ db.GetTopUsersByOrgParams) ([]db.GetTopUsersByOrgRow, error) {
	return m.userRows, m.userErr
}

func (m *usageMock) GetTopModelsByOrg(_ context.Context, _ db.GetTopModelsByOrgParams) ([]db.GetTopModelsByOrgRow, error) {
	return m.modelRows, m.modelErr
}

// newUsageTestRouter builds a chi router that exposes only the usage page handler
// without auth/org middleware, so tests can inject context directly.
func newUsageTestRouter(q db.Querier) *chi.Mux {
	store := sessions.NewCookieStore([]byte("test-secret-32bytes-for-testing!"))
	authHandler := auth.NewWithStore(store, nil)
	h := New(authHandler, q)
	r := chi.NewRouter()
	// Mount the handler directly without auth/org middleware — tests set context.
	r.Get("/portal/usage", h.usagePageHandler)
	return r
}

func TestUsagePage_Renders(t *testing.T) {
	mock := &usageMock{
		dailyRows: []db.GetDailyUsageByOrgRow{
			{
				Day:          mustDate("2026-03-01"),
				InputTokens:  100,
				OutputTokens: 200,
				CostUsd:      mustNumeric(0.05),
				RequestCount: 3,
			},
		},
		userRows: []db.GetTopUsersByOrgRow{
			{
				ID:            mustUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				Email:         "alice@example.com",
				TotalCostUsd:  mustNumeric(0.05),
				TotalRequests: 3,
			},
		},
		modelRows: []db.GetTopModelsByOrgRow{
			{
				Model:         "gpt-4o",
				Provider:      "openai",
				TotalCostUsd:  mustNumeric(0.05),
				TotalRequests: 3,
			},
		},
	}

	r := newUsageTestRouter(mock)
	req := httptest.NewRequest(http.MethodGet, "/portal/usage", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Usage Dashboard") {
		t.Errorf("expected 'Usage Dashboard' in body")
	}
	if !strings.Contains(body, "Daily Usage") {
		t.Errorf("expected 'Daily Usage' table header in body")
	}
	if !strings.Contains(body, "Top Users") {
		t.Errorf("expected 'Top Users' table header in body")
	}
	if !strings.Contains(body, "Top Models") {
		t.Errorf("expected 'Top Models' table header in body")
	}
	if !strings.Contains(body, "alice@example.com") {
		t.Errorf("expected user email in body")
	}
	if !strings.Contains(body, "gpt-4o") {
		t.Errorf("expected model name in body")
	}
	if !strings.Contains(body, "2026-03-01") {
		t.Errorf("expected date in body")
	}
}

func TestUsagePage_EmptyData(t *testing.T) {
	mock := &usageMock{
		dailyRows: []db.GetDailyUsageByOrgRow{},
		userRows:  []db.GetTopUsersByOrgRow{},
		modelRows: []db.GetTopModelsByOrgRow{},
	}

	r := newUsageTestRouter(mock)
	req := httptest.NewRequest(http.MethodGet, "/portal/usage", nil)
	req = withPortalContext(req, "member")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Usage Dashboard") {
		t.Errorf("expected 'Usage Dashboard' in body")
	}
	if !strings.Contains(body, "No usage data yet") {
		t.Errorf("expected 'No usage data yet' placeholder in daily usage section")
	}
	if !strings.Contains(body, "No data yet") {
		t.Errorf("expected 'No data yet' placeholder in top users/models sections")
	}
	if strings.Contains(body, "2026-03-01") {
		t.Errorf("did not expect non-empty usage data (e.g., date '2026-03-01') in body for empty sections")
	}
}

func TestUsagePage_DBError(t *testing.T) {
	mock := &usageMock{
		dailyErr: fmt.Errorf("db connection error"),
	}

	r := newUsageTestRouter(mock)
	req := httptest.NewRequest(http.MethodGet, "/portal/usage", nil)
	req = withPortalContext(req, "org_admin")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to load usage data") {
		t.Errorf("expected error message in body")
	}
}
