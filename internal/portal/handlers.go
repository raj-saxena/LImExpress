package portal

import (
	"bytes"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/keys"
	"github.com/limexpress/gateway/internal/portal/auth"
	"github.com/limexpress/gateway/internal/portal/templates"
)

// Handler wraps portal template rendering and auth route delegation.
type Handler struct {
	authHandler *auth.Handler
	querier     db.Querier
}

// New returns a new portal Handler.
func New(authHandler *auth.Handler, querier db.Querier) *Handler {
	return &Handler{authHandler: authHandler, querier: querier}
}

// RegisterRoutes mounts all portal and auth routes on the given chi router.
//
// Public routes (no session required):
//   - GET  /                     → redirect to /auth/login-page
//   - GET  /auth/login-page      → login UI (shows Google button)
//   - GET  /auth/login           → initiate OIDC flow (redirect to Google)
//   - GET  /auth/callback        → OAuth2 callback from Google
//   - POST /auth/logout          → clear session, redirect to login page
//
// Auth-only (session required, no org needed):
//   - GET  /portal/access-denied → shown when user has no org memberships
//
// Auth + org middleware:
//   - GET    /portal             → dashboard
//   - POST   /portal/switch-org  → change active org in session
//   - GET    /portal/keys        → key management UI
//   - POST   /portal/keys        → create a key (HTMX, returns HTML partial)
//   - DELETE /portal/keys/{id}   → revoke a key (HTMX, returns 200)
func (h *Handler) RegisterRoutes(r chi.Router) {
	// Root redirect.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/auth/login-page", http.StatusFound)
	})

	// Public auth routes.
	r.Get("/auth/login-page", h.loginPageHandler)
	r.Get("/auth/login", h.authHandler.LoginHandler)
	r.Get("/auth/callback", h.authHandler.CallbackHandler)
	r.Post("/auth/logout", h.authHandler.LogoutHandler)

	// Auth-required but no org context (e.g. user has no org memberships).
	r.Group(func(r chi.Router) {
		r.Use(h.authHandler.RequireAuth)
		r.Get("/portal/access-denied", h.accessDeniedHandler)
	})

	// Auth-required + org context.
	r.Group(func(r chi.Router) {
		r.Use(h.authHandler.RequireAuth)
		r.Use(h.authHandler.OrgMiddleware(h.querier))
		r.Get("/portal", h.indexHandler)
		r.Post("/portal/switch-org", h.authHandler.SwitchOrgHandler(h.querier))
		r.Get("/portal/keys", h.keysPageHandler)
		r.Post("/portal/keys", h.createKeyHandler)
		r.Delete("/portal/keys/{id}", h.revokeKeyHandler)
	})
}

// loginPageHandler renders the sign-in page.
// Reads ?signed_out=1 to show a "signed out" success flash.
func (h *Handler) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	signedOut := r.URL.Query().Get("signed_out") == "1"
	var buf bytes.Buffer
	if err := templates.Login(signedOut).Render(r.Context(), &buf); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// indexHandler renders the portal dashboard.
// Reads the authenticated user's email and active org from context.
func (h *Handler) indexHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
	}
	orgName := ""
	if o, ok := auth.OrgFromContext(r.Context()); ok {
		orgName = o.Name
	}
	var buf bytes.Buffer
	if err := templates.Index(userEmail, orgName).Render(r.Context(), &buf); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// accessDeniedHandler renders the access-denied page for authenticated users
// who have no org memberships.
func (h *Handler) accessDeniedHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
	}
	var buf bytes.Buffer
	if err := templates.AccessDenied(userEmail).Render(r.Context(), &buf); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// keysPageHandler renders the API Keys management page.
// Admins see all org keys; members see only their own.
func (h *Handler) keysPageHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	var user *auth.UserContext
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
		user = u
	}
	orgName := ""
	var org *auth.OrgContext
	if o, ok := auth.OrgFromContext(r.Context()); ok {
		orgName = o.Name
		org = o
	}

	isAdmin := org != nil && org.Role == "org_admin"
	var keyRows []templates.KeyRow

	if h.querier != nil && user != nil && org != nil {
		if isAdmin {
			rows, err := h.querier.ListKeysByOrg(r.Context(), org.OrgID)
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			keyRows = make([]templates.KeyRow, 0, len(rows))
			for _, row := range rows {
				keyRows = append(keyRows, templates.KeyRow{
					ID:         uuidToString(row.ID),
					Prefix:     row.Prefix,
					Status:     row.Status,
					CreatedAt:  timeToRFC3339(row.CreatedAt),
					LastUsedAt: optionalTime(row.LastUsedAt),
					RevokedAt:  optionalTime(row.RevokedAt),
				})
			}
		} else {
			rows, err := h.querier.ListVirtualKeysByUser(r.Context(), db.ListVirtualKeysByUserParams{
				UserID: user.UserID,
				OrgID:  org.OrgID,
			})
			if err != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			keyRows = make([]templates.KeyRow, 0, len(rows))
			for _, row := range rows {
				keyRows = append(keyRows, templates.KeyRow{
					ID:         uuidToString(row.ID),
					Prefix:     row.Prefix,
					Status:     row.Status,
					CreatedAt:  timeToRFC3339(row.CreatedAt),
					LastUsedAt: optionalTime(row.LastUsedAt),
					RevokedAt:  optionalTime(row.RevokedAt),
				})
			}
		}
	}

	if keyRows == nil {
		keyRows = []templates.KeyRow{}
	}

	var buf bytes.Buffer
	if err := templates.Keys(userEmail, orgName, isAdmin, keyRows).Render(r.Context(), &buf); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// createKeyHandler creates a new virtual API key for the authenticated admin user.
// Returns an HTML partial (HTMX) revealing the plaintext key once.
func (h *Handler) createKeyHandler(w http.ResponseWriter, r *http.Request) {
	user, org, ok := requirePortalContext(w, r)
	if !ok {
		return
	}
	if org.Role != "org_admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	plaintext, sha256hex, prefix, err := keys.Generate()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	created, err := h.querier.CreateVirtualKey(r.Context(), db.CreateVirtualKeyParams{
		OrgID:   org.OrgID,
		UserID:  user.UserID,
		TeamID:  pgtype.UUID{},
		KeyHash: sha256hex,
		Prefix:  prefix,
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	if err := templates.KeyCreatedPartial(plaintext, created.Prefix, uuidToString(created.ID)).Render(r.Context(), &buf); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// revokeKeyHandler revokes a virtual API key by ID.
// Returns 200 OK on success; HTMX swaps out the row.
func (h *Handler) revokeKeyHandler(w http.ResponseWriter, r *http.Request) {
	_, org, ok := requirePortalContext(w, r)
	if !ok {
		return
	}
	if org.Role != "org_admin" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	keyID, err := parseUUID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	if err := h.querier.RevokeVirtualKey(r.Context(), db.RevokeVirtualKeyParams{ID: keyID, OrgID: org.OrgID}); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
