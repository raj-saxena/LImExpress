package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/limexpress/gateway/internal/db"
)

// ---------------------------------------------------------------------------
// Minimal DB mock — only GetUserOrgs is implemented; all others panic.
// ---------------------------------------------------------------------------

type mockQuerier struct {
	getUserOrgsResult []db.GetUserOrgsRow
	getUserOrgsErr    error
}

func (m *mockQuerier) GetUserOrgs(_ context.Context, _ pgtype.UUID) ([]db.GetUserOrgsRow, error) {
	return m.getUserOrgsResult, m.getUserOrgsErr
}

func (m *mockQuerier) CreateVirtualKey(_ context.Context, _ db.CreateVirtualKeyParams) (db.VirtualKey, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetBudgetPolicy(_ context.Context, _ db.GetBudgetPolicyParams) (db.BudgetPolicy, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetCurrentWindowUsageDay(_ context.Context, _ db.GetCurrentWindowUsageDayParams) (db.GetCurrentWindowUsageDayRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetCurrentWindowUsageHour(_ context.Context, _ db.GetCurrentWindowUsageHourParams) (db.GetCurrentWindowUsageHourRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetDailyUsageByOrg(_ context.Context, _ db.GetDailyUsageByOrgParams) ([]db.GetDailyUsageByOrgRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetTeamWindowUsageDay(_ context.Context, _ db.GetTeamWindowUsageDayParams) (db.GetTeamWindowUsageDayRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetTeamWindowUsageHour(_ context.Context, _ db.GetTeamWindowUsageHourParams) (db.GetTeamWindowUsageHourRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetTopModelsByOrg(_ context.Context, _ db.GetTopModelsByOrgParams) ([]db.GetTopModelsByOrgRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetTopUsersByOrg(_ context.Context, _ db.GetTopUsersByOrgParams) ([]db.GetTopUsersByOrgRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetUserByEmail(_ context.Context, _ string) (db.User, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetVirtualKey(_ context.Context, _ db.GetVirtualKeyParams) (db.GetVirtualKeyRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) GetVirtualKeyByHash(_ context.Context, _ string) (db.VirtualKey, error) {
	panic("not implemented")
}
func (m *mockQuerier) InsertUsageEvent(_ context.Context, _ db.InsertUsageEventParams) (db.InsertUsageEventRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) ListKeysByOrg(_ context.Context, _ pgtype.UUID) ([]db.ListKeysByOrgRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) ListVirtualKeysByUser(_ context.Context, _ db.ListVirtualKeysByUserParams) ([]db.ListVirtualKeysByUserRow, error) {
	panic("not implemented")
}
func (m *mockQuerier) RevokeVirtualKey(_ context.Context, _ db.RevokeVirtualKeyParams) error {
	panic("not implemented")
}
func (m *mockQuerier) UpdateVirtualKeyLastUsed(_ context.Context, _ db.UpdateVirtualKeyLastUsedParams) error {
	panic("not implemented")
}
func (m *mockQuerier) UpsertUsageAggDay(_ context.Context, _ db.UpsertUsageAggDayParams) error {
	panic("not implemented")
}
func (m *mockQuerier) UpsertUsageAggHour(_ context.Context, _ db.UpsertUsageAggHourParams) error {
	panic("not implemented")
}
func (m *mockQuerier) UpsertUser(_ context.Context, _ string) (db.User, error) {
	panic("not implemented")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// mustParseUUID parses a UUID string and panics on error (test helper).
func mustParseUUID(s string) pgtype.UUID {
	u, err := stringToUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// newTestHandler builds a Handler with a test-only cookie store.
func newTestHandler() *Handler {
	store := sessions.NewCookieStore([]byte("test-secret"))
	return &Handler{store: store}
}

// newRequestWithUserCtx builds an httptest.Request pre-populated with a UserContext.
func newRequestWithUserCtx(method, target string, userID pgtype.UUID, email string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	ctx := context.WithValue(r.Context(), userContextKey, &UserContext{
		UserID: userID,
		Email:  email,
	})
	return r.WithContext(ctx)
}

// saveOrgIDInSession writes active_org_id into the session cookie of the given
// request and returns a new request carrying that cookie.
func saveOrgIDInSession(t *testing.T, h *Handler, r *http.Request, orgIDStr string) *http.Request {
	t.Helper()
	rec := httptest.NewRecorder()
	sess, err := h.store.Get(r, sessionName)
	if err != nil {
		t.Fatalf("saveOrgIDInSession: get session: %v", err)
	}
	sess.Values["active_org_id"] = orgIDStr
	if err := sess.Save(r, rec); err != nil {
		t.Fatalf("saveOrgIDInSession: save session: %v", err)
	}
	// Copy the Set-Cookie from the recorder back into a new request.
	newReq := r.Clone(r.Context())
	for _, c := range rec.Result().Cookies() {
		newReq.AddCookie(c)
	}
	return newReq
}

// ---------------------------------------------------------------------------
// OrgFromContext tests
// ---------------------------------------------------------------------------

func TestOrgFromContext_Empty(t *testing.T) {
	ctx := context.Background()
	o, ok := OrgFromContext(ctx)
	if ok {
		t.Fatalf("expected ok=false for empty context, got ok=true with %+v", o)
	}
	if o != nil {
		t.Fatalf("expected nil OrgContext for empty context, got %+v", o)
	}
}

func TestOrgFromContext_Set(t *testing.T) {
	orgID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	want := &OrgContext{OrgID: orgID, Name: "Acme", Role: "org_admin"}
	ctx := context.WithValue(context.Background(), orgContextKey, want)
	got, ok := OrgFromContext(ctx)
	if !ok {
		t.Fatal("expected ok=true, got false")
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

// ---------------------------------------------------------------------------
// OrgMiddleware tests
// ---------------------------------------------------------------------------

func TestOrgMiddleware_NoUserContext_RedirectsToLogin(t *testing.T) {
	h := newTestHandler()
	mock := &mockQuerier{}

	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called")
	})

	middleware := h.OrgMiddleware(mock)(sentinel)
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestOrgMiddleware_NoOrgs_RedirectsToAccessDenied(t *testing.T) {
	h := newTestHandler()
	mock := &mockQuerier{getUserOrgsResult: nil}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	sentinel := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("inner handler should not be called")
	})

	middleware := h.OrgMiddleware(mock)(sentinel)
	req := newRequestWithUserCtx(http.MethodGet, "/portal", userID, "user@example.com")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/portal/access-denied" {
		t.Fatalf("expected redirect to /portal/access-denied, got %q", loc)
	}
}

func TestOrgMiddleware_OneOrg_AutoSelects(t *testing.T) {
	h := newTestHandler()
	orgID := mustParseUUID("22222222-2222-2222-2222-222222222222")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: orgID, Name: "Acme", Role: "member"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var capturedOrg *OrgContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrg, _ = OrgFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := h.OrgMiddleware(mock)(inner)
	req := newRequestWithUserCtx(http.MethodGet, "/portal", userID, "user@example.com")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if capturedOrg == nil {
		t.Fatal("expected OrgContext to be injected, got nil")
	}
	if uuidToString(capturedOrg.OrgID) != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("unexpected OrgID: %s", uuidToString(capturedOrg.OrgID))
	}
	if capturedOrg.Name != "Acme" {
		t.Fatalf("unexpected Name: %s", capturedOrg.Name)
	}
	if capturedOrg.Role != "member" {
		t.Fatalf("unexpected Role: %s", capturedOrg.Role)
	}
}

func TestOrgMiddleware_HeaderOverride_ValidOrg(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	org2ID := mustParseUUID("22222222-2222-2222-2222-222222222222")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
			{ID: org2ID, Name: "Org Two", Role: "org_admin"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var capturedOrg *OrgContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrg, _ = OrgFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := h.OrgMiddleware(mock)(inner)
	req := newRequestWithUserCtx(http.MethodGet, "/portal", userID, "user@example.com")
	req.Header.Set("X-Active-Org", "22222222-2222-2222-2222-222222222222")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if capturedOrg == nil {
		t.Fatal("expected OrgContext to be injected, got nil")
	}
	if uuidToString(capturedOrg.OrgID) != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected org2, got %s", uuidToString(capturedOrg.OrgID))
	}
	if capturedOrg.Role != "org_admin" {
		t.Fatalf("expected org_admin role, got %s", capturedOrg.Role)
	}
}

func TestOrgMiddleware_HeaderOverride_InvalidOrg_FallsBackToFirst(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var capturedOrg *OrgContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrg, _ = OrgFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := h.OrgMiddleware(mock)(inner)
	req := newRequestWithUserCtx(http.MethodGet, "/portal", userID, "user@example.com")
	// Header contains an org UUID that is NOT in the user's membership.
	req.Header.Set("X-Active-Org", "ffffffff-ffff-ffff-ffff-ffffffffffff")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if capturedOrg == nil {
		t.Fatal("expected OrgContext to be injected, got nil")
	}
	// Falls back to first org.
	if uuidToString(capturedOrg.OrgID) != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected fallback to org1, got %s", uuidToString(capturedOrg.OrgID))
	}
}

func TestOrgMiddleware_SessionOrgID_UsedWhenNoHeader(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	org2ID := mustParseUUID("22222222-2222-2222-2222-222222222222")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
			{ID: org2ID, Name: "Org Two", Role: "org_admin"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var capturedOrg *OrgContext
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrg, _ = OrgFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := h.OrgMiddleware(mock)(inner)
	// Build request with UserContext and session org_id set to org2.
	req := newRequestWithUserCtx(http.MethodGet, "/portal", userID, "user@example.com")
	req = saveOrgIDInSession(t, h, req, "22222222-2222-2222-2222-222222222222")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rec.Code)
	}
	if capturedOrg == nil {
		t.Fatal("expected OrgContext to be injected, got nil")
	}
	if uuidToString(capturedOrg.OrgID) != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected org2, got %s", uuidToString(capturedOrg.OrgID))
	}
}

// ---------------------------------------------------------------------------
// SwitchOrgHandler tests
// ---------------------------------------------------------------------------

func TestSwitchOrgHandler_ValidOrg_SavesSessionAndRedirects(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	org2ID := mustParseUUID("22222222-2222-2222-2222-222222222222")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
			{ID: org2ID, Name: "Org Two", Role: "org_admin"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	handler := h.SwitchOrgHandler(mock)

	form := url.Values{"org_id": {"22222222-2222-2222-2222-222222222222"}}
	req := newRequestWithUserCtx(http.MethodPost, "/portal/switch-org",
		userID, "user@example.com")
	req.Body = http.NoBody
	req = httptest.NewRequest(http.MethodPost, "/portal/switch-org",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: userID,
		Email:  "user@example.com",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/portal" {
		t.Fatalf("expected redirect to /portal, got %q", loc)
	}

	// Verify the session cookie was set with the correct org_id.
	cookieReq := httptest.NewRequest(http.MethodGet, "/portal", nil)
	for _, c := range rec.Result().Cookies() {
		cookieReq.AddCookie(c)
	}
	sess, err := h.store.Get(cookieReq, sessionName)
	if err != nil {
		t.Fatalf("reading session: %v", err)
	}
	saved, ok := sess.Values["active_org_id"].(string)
	if !ok {
		t.Fatal("active_org_id not found in session")
	}
	if saved != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected org2 in session, got %q", saved)
	}
}

func TestSwitchOrgHandler_OrgNotInMembership_Returns403(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	handler := h.SwitchOrgHandler(mock)

	form := url.Values{"org_id": {"ffffffff-ffff-ffff-ffff-ffffffffffff"}}
	req := httptest.NewRequest(http.MethodPost, "/portal/switch-org",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: userID,
		Email:  "user@example.com",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestSwitchOrgHandler_NoUserContext_RedirectsToLogin(t *testing.T) {
	h := newTestHandler()
	mock := &mockQuerier{}
	handler := h.SwitchOrgHandler(mock)

	form := url.Values{"org_id": {"11111111-1111-1111-1111-111111111111"}}
	req := httptest.NewRequest(http.MethodPost, "/portal/switch-org",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

func TestSwitchOrgHandler_SameOriginReferer_RedirectsToReferer(t *testing.T) {
	h := newTestHandler()
	org1ID := mustParseUUID("11111111-1111-1111-1111-111111111111")
	mock := &mockQuerier{
		getUserOrgsResult: []db.GetUserOrgsRow{
			{ID: org1ID, Name: "Org One", Role: "member"},
		},
	}

	userID := mustParseUUID("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	handler := h.SwitchOrgHandler(mock)

	form := url.Values{"org_id": {"11111111-1111-1111-1111-111111111111"}}
	req := httptest.NewRequest(http.MethodPost, "/portal/switch-org",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "/portal/dashboard")
	ctx := context.WithValue(req.Context(), userContextKey, &UserContext{
		UserID: userID,
		Email:  "user@example.com",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/portal/dashboard" {
		t.Fatalf("expected redirect to /portal/dashboard, got %q", loc)
	}
}
