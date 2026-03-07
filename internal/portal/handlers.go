package portal

import (
	"bytes"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/limexpress/gateway/internal/db"
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
//   - GET  /portal               → dashboard
//   - POST /portal/switch-org    → change active org in session
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
