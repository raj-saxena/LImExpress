package budget_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/budget"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/middleware"
)

// ----------------------------------------------------------------------------
// Test fixtures / helpers
// ----------------------------------------------------------------------------

// numericFromFloat64 builds a valid pgtype.Numeric from a decimal value.
func numericFromFloat64(v float64) pgtype.Numeric {
	n := pgtype.Numeric{}
	_ = n.Scan(strconv.FormatFloat(v, 'f', 8, 64))
	return n
}

func invalidNumeric() pgtype.Numeric {
	return pgtype.Numeric{Valid: false}
}

func int64Ptr(v int64) *int64 { return &v }

// fixedUUID returns a non-zero deterministic UUID for testing.
func fixedUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[0] = b
	u.Valid = true
	return u
}

// zeroUUID is the zero-value UUID — represents "no team".
func zeroUUID() pgtype.UUID { return pgtype.UUID{} }

// defaultKAC returns a KeyAuthContext with no team.
func defaultKAC() *middleware.KeyAuthContext {
	return &middleware.KeyAuthContext{
		OrgID:  fixedUUID(1),
		UserID: fixedUUID(2),
		TeamID: zeroUUID(),
	}
}

// teamKAC returns a KeyAuthContext with a non-zero team.
func teamKAC() *middleware.KeyAuthContext {
	return &middleware.KeyAuthContext{
		OrgID:  fixedUUID(1),
		UserID: fixedUUID(2),
		TeamID: fixedUUID(3),
	}
}

// injectAuth wraps next with a handler that inserts kac into the request context
// using the same internal key as VirtualKeyAuth (via the exported test helper).
func injectAuth(kac *middleware.KeyAuthContext, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithTestContext(r.Context(), kac)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// runRequest builds the middleware chain (auth injection → BudgetAdmission → ok handler)
// and fires a GET request, returning the recorded response.
func runRequest(t *testing.T, q db.Querier, kac *middleware.KeyAuthContext) *httptest.ResponseRecorder {
	t.Helper()
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := injectAuth(kac, budget.BudgetAdmission(q)(downstream))
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)
	return w
}

// runRequestNoAuth fires a request through BudgetAdmission without any auth context.
func runRequestNoAuth(t *testing.T, q db.Querier) *httptest.ResponseRecorder {
	t.Helper()
	downstream := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := budget.BudgetAdmission(q)(downstream)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)
	return w
}

func assertPassThrough(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 pass-through, got %d; body: %s", w.Code, w.Body.String())
	}
}

func assertDenied(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d; body: %s", w.Code, w.Body.String())
	}

	// Retry-After must be present and a positive integer.
	ra := w.Header().Get("Retry-After")
	if ra == "" {
		t.Fatal("Retry-After header missing on 429 response")
	}
	n, err := strconv.ParseInt(ra, 10, 64)
	if err != nil || n <= 0 {
		t.Fatalf("Retry-After must be a positive integer, got %q", ra)
	}

	// JSON body must contain correct fields.
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("could not decode response body: %v", err)
	}
	if body["error"] != "budget_exceeded" {
		t.Fatalf("expected error=budget_exceeded, got %v", body["error"])
	}
	if _, ok := body["retry_after_seconds"]; !ok {
		t.Fatal("retry_after_seconds missing from response body")
	}
}

// ----------------------------------------------------------------------------
// Mock db.Querier — only budget/usage methods return configured data;
// all other methods return zero values.
// ----------------------------------------------------------------------------

type mockQuerier struct {
	policyErr   error
	policy      db.BudgetPolicy
	userHourRow db.GetCurrentWindowUsageHourRow
	userHourErr error
	userDayRow  db.GetCurrentWindowUsageDayRow
	userDayErr  error
	teamHourRow db.GetTeamWindowUsageHourRow
	teamHourErr error
	teamDayRow  db.GetTeamWindowUsageDayRow
	teamDayErr  error
}

func (m *mockQuerier) GetBudgetPolicy(_ context.Context, _ db.GetBudgetPolicyParams) (db.BudgetPolicy, error) {
	return m.policy, m.policyErr
}
func (m *mockQuerier) GetCurrentWindowUsageHour(_ context.Context, _ db.GetCurrentWindowUsageHourParams) (db.GetCurrentWindowUsageHourRow, error) {
	return m.userHourRow, m.userHourErr
}
func (m *mockQuerier) GetCurrentWindowUsageDay(_ context.Context, _ db.GetCurrentWindowUsageDayParams) (db.GetCurrentWindowUsageDayRow, error) {
	return m.userDayRow, m.userDayErr
}
func (m *mockQuerier) GetTeamWindowUsageHour(_ context.Context, _ db.GetTeamWindowUsageHourParams) (db.GetTeamWindowUsageHourRow, error) {
	return m.teamHourRow, m.teamHourErr
}
func (m *mockQuerier) GetTeamWindowUsageDay(_ context.Context, _ db.GetTeamWindowUsageDayParams) (db.GetTeamWindowUsageDayRow, error) {
	return m.teamDayRow, m.teamDayErr
}

// Remaining interface methods — not exercised by budget tests.
func (m *mockQuerier) CreateVirtualKey(_ context.Context, _ db.CreateVirtualKeyParams) (db.VirtualKey, error) {
	return db.VirtualKey{}, nil
}
func (m *mockQuerier) GetDailyUsageByOrg(_ context.Context, _ db.GetDailyUsageByOrgParams) ([]db.GetDailyUsageByOrgRow, error) {
	return nil, nil
}
func (m *mockQuerier) GetTopModelsByOrg(_ context.Context, _ db.GetTopModelsByOrgParams) ([]db.GetTopModelsByOrgRow, error) {
	return nil, nil
}
func (m *mockQuerier) GetTopUsersByOrg(_ context.Context, _ db.GetTopUsersByOrgParams) ([]db.GetTopUsersByOrgRow, error) {
	return nil, nil
}
func (m *mockQuerier) GetUserByEmail(_ context.Context, _ string) (db.User, error) {
	return db.User{}, nil
}
func (m *mockQuerier) GetUserOrgs(_ context.Context, _ pgtype.UUID) ([]db.GetUserOrgsRow, error) {
	return nil, nil
}
func (m *mockQuerier) GetVirtualKey(_ context.Context, _ db.GetVirtualKeyParams) (db.GetVirtualKeyRow, error) {
	return db.GetVirtualKeyRow{}, nil
}
func (m *mockQuerier) GetVirtualKeyByHash(_ context.Context, _ string) (db.VirtualKey, error) {
	return db.VirtualKey{}, nil
}
func (m *mockQuerier) InsertUsageEvent(_ context.Context, _ db.InsertUsageEventParams) (db.InsertUsageEventRow, error) {
	return db.InsertUsageEventRow{}, nil
}
func (m *mockQuerier) ListKeysByOrg(_ context.Context, _ pgtype.UUID) ([]db.ListKeysByOrgRow, error) {
	return nil, nil
}
func (m *mockQuerier) ListVirtualKeysByUser(_ context.Context, _ db.ListVirtualKeysByUserParams) ([]db.ListVirtualKeysByUserRow, error) {
	return nil, nil
}
func (m *mockQuerier) RevokeVirtualKey(_ context.Context, _ db.RevokeVirtualKeyParams) error {
	return nil
}
func (m *mockQuerier) UpdateVirtualKeyLastUsed(_ context.Context, _ db.UpdateVirtualKeyLastUsedParams) error {
	return nil
}
func (m *mockQuerier) UpsertUsageAggDay(_ context.Context, _ db.UpsertUsageAggDayParams) error {
	return nil
}
func (m *mockQuerier) UpsertUsageAggHour(_ context.Context, _ db.UpsertUsageAggHourParams) error {
	return nil
}
func (m *mockQuerier) UpsertUser(_ context.Context, _ string) (db.User, error) {
	return db.User{}, nil
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

// No auth context in request → BudgetAdmission must pass through.
func TestBudgetAdmission_NoAuthContext_PassThrough(t *testing.T) {
	q := &mockQuerier{policyErr: pgx.ErrNoRows}
	w := runRequestNoAuth(t, q)
	assertPassThrough(t, w)
}

// Auth context present but no policy row → pass through (no limit).
func TestBudgetAdmission_NoPolicyFound_PassThrough(t *testing.T) {
	q := &mockQuerier{policyErr: pgx.ErrNoRows}
	w := runRequest(t, q, defaultKAC())
	assertPassThrough(t, w)
}

// Policy exists with all nil/invalid limits → always pass through.
func TestBudgetAdmission_PolicyAllNilLimits_PassThrough(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  invalidNumeric(),
			MaxTokensHour:  nil,
			MaxTokensDay:   nil,
		},
		// Usage well over any conceivable limit — but limits are nil, so pass.
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(99999), TotalTokens: 99999},
		userDayRow:  db.GetCurrentWindowUsageDayRow{CostUsd: numericFromFloat64(99999), TotalTokens: 99999},
	}
	w := runRequest(t, q, defaultKAC())
	assertPassThrough(t, w)
}

// Usage is below all limits → pass through.
func TestBudgetAdmission_UsageUnderAllLimits_PassThrough(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(10.0),
			MaxCostUsdDay:  numericFromFloat64(100.0),
			MaxTokensHour:  int64Ptr(1000),
			MaxTokensDay:   int64Ptr(5000),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(1.0), TotalTokens: 100},
		userDayRow:  db.GetCurrentWindowUsageDayRow{CostUsd: numericFromFloat64(5.0), TotalTokens: 500},
	}
	w := runRequest(t, q, defaultKAC())
	assertPassThrough(t, w)
}

// User hour cost exactly at limit (exhausted) → 429.
func TestBudgetAdmission_UserHourCostExceeded_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(10.0),
			MaxCostUsdDay:  invalidNumeric(),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(10.0)},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)
}

// User day cost over limit → 429.
func TestBudgetAdmission_UserDayCostExceeded_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  numericFromFloat64(50.0),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{},
		userDayRow:  db.GetCurrentWindowUsageDayRow{CostUsd: numericFromFloat64(75.0)},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)
}

// User hour tokens exactly at limit → 429.
func TestBudgetAdmission_UserHourTokensExceeded_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  invalidNumeric(),
			MaxTokensHour:  int64Ptr(500),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{TotalTokens: 500},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)
}

// User day tokens over limit → 429.
func TestBudgetAdmission_UserDayTokensExceeded_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  invalidNumeric(),
			MaxTokensDay:   int64Ptr(10000),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{},
		userDayRow:  db.GetCurrentWindowUsageDayRow{TotalTokens: 20000},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)
}

// Team hour tokens over limit (user is under) → 429 (smallest-remaining-wins).
func TestBudgetAdmission_TeamHourTokensExceeded_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  invalidNumeric(),
			MaxTokensHour:  int64Ptr(500),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{TotalTokens: 100},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
		teamHourRow: db.GetTeamWindowUsageHourRow{TotalTokens: 600},
		teamDayRow:  db.GetTeamWindowUsageDayRow{},
	}
	w := runRequest(t, q, teamKAC())
	assertDenied(t, w)
}

// Only team exhausted (user has remaining) → 429.
func TestBudgetAdmission_OnlyTeamExhausted_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(10.0),
			MaxCostUsdDay:  invalidNumeric(),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(1.0)},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
		teamHourRow: db.GetTeamWindowUsageHourRow{CostUsd: numericFromFloat64(15.0)},
		teamDayRow:  db.GetTeamWindowUsageDayRow{},
	}
	w := runRequest(t, q, teamKAC())
	assertDenied(t, w)
}

// Only user exhausted (team has remaining) → 429.
func TestBudgetAdmission_OnlyUserExhausted_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(10.0),
			MaxCostUsdDay:  invalidNumeric(),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(12.0)},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
		teamHourRow: db.GetTeamWindowUsageHourRow{CostUsd: numericFromFloat64(2.0)},
		teamDayRow:  db.GetTeamWindowUsageDayRow{},
	}
	w := runRequest(t, q, teamKAC())
	assertDenied(t, w)
}

// Both user and team at/over limit → 429.
func TestBudgetAdmission_BothExhausted_Deny429(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  invalidNumeric(),
			MaxTokensHour:  int64Ptr(0), // limit of 0 means any usage exhausts it
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{TotalTokens: 1},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
		teamHourRow: db.GetTeamWindowUsageHourRow{TotalTokens: 1},
		teamDayRow:  db.GetTeamWindowUsageDayRow{},
	}
	w := runRequest(t, q, teamKAC())
	assertDenied(t, w)
}

// Both user and team under all limits → pass through.
func TestBudgetAdmission_BothUnderLimit_WithTeam_PassThrough(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(10.0),
			MaxCostUsdDay:  numericFromFloat64(100.0),
			MaxTokensHour:  int64Ptr(1000),
			MaxTokensDay:   int64Ptr(5000),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(1.0), TotalTokens: 100},
		userDayRow:  db.GetCurrentWindowUsageDayRow{CostUsd: numericFromFloat64(5.0), TotalTokens: 200},
		teamHourRow: db.GetTeamWindowUsageHourRow{CostUsd: numericFromFloat64(2.0), TotalTokens: 150},
		teamDayRow:  db.GetTeamWindowUsageDayRow{CostUsd: numericFromFloat64(8.0), TotalTokens: 300},
	}
	w := runRequest(t, q, teamKAC())
	assertPassThrough(t, w)
}

// Hour window denial: Retry-After must be in (0, 3601].
func TestBudgetAdmission_RetryAfterHeader_HourWindow(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: numericFromFloat64(5.0),
			MaxCostUsdDay:  invalidNumeric(),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{CostUsd: numericFromFloat64(10.0)},
		userDayRow:  db.GetCurrentWindowUsageDayRow{},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)

	ra, err := strconv.ParseInt(w.Header().Get("Retry-After"), 10, 64)
	if err != nil {
		t.Fatalf("Retry-After parse error: %v", err)
	}
	if ra <= 0 || ra > 3601 {
		t.Fatalf("Retry-After for hour window should be in (0, 3601], got %d", ra)
	}
}

// Day window denial: Retry-After must be in (0, 86401].
func TestBudgetAdmission_RetryAfterHeader_DayWindow(t *testing.T) {
	q := &mockQuerier{
		policy: db.BudgetPolicy{
			MaxCostUsdHour: invalidNumeric(),
			MaxCostUsdDay:  numericFromFloat64(5.0),
		},
		userHourRow: db.GetCurrentWindowUsageHourRow{},
		userDayRow:  db.GetCurrentWindowUsageDayRow{CostUsd: numericFromFloat64(10.0)},
	}
	w := runRequest(t, q, defaultKAC())
	assertDenied(t, w)

	ra, err := strconv.ParseInt(w.Header().Get("Retry-After"), 10, 64)
	if err != nil {
		t.Fatalf("Retry-After parse error: %v", err)
	}
	if ra <= 0 || ra > 86401 {
		t.Fatalf("Retry-After for day window should be in (0, 86401], got %d", ra)
	}
}
