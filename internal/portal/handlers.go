package portal

import (
	"bytes"
	"net/http"
	"time"

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
//   - GET  /portal/usage         → usage dashboard (last 90 days)
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
		r.Get("/portal/usage", h.usagePageHandler)
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

// usagePageHandler renders the usage dashboard page with daily usage,
// top users, and top models for the last 90 days.
func (h *Handler) usagePageHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
	}
	orgName := ""
	var org *auth.OrgContext
	if o, ok := auth.OrgFromContext(r.Context()); ok {
		orgName = o.Name
		org = o
	}

	var daily []DailyRow
	var topUsers []TopUserRow
	var topModels []TopModelRow

	if h.querier != nil && org != nil {
		now := time.Now().UTC()
		from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -90)
		to := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

		if rows, err := h.querier.GetDailyUsageByOrg(r.Context(), db.GetDailyUsageByOrgParams{
			OrgID:         org.OrgID,
			WindowStart:   dashboardToTimestamptz(from),
			WindowStart_2: dashboardToTimestamptz(to),
		}); err == nil {
			daily = make([]DailyRow, 0, len(rows))
			for _, row := range rows {
				daily = append(daily, DailyRow{
					Day:          dashboardDateToString(row.Day),
					InputTokens:  row.InputTokens,
					OutputTokens: row.OutputTokens,
					CostUSD:      dashboardNumericToFloat64(row.CostUsd),
					RequestCount: row.RequestCount,
				})
			}
		}

		if rows, err := h.querier.GetTopUsersByOrg(r.Context(), db.GetTopUsersByOrgParams{
			OrgID:         org.OrgID,
			WindowStart:   dashboardToTimestamptz(from),
			WindowStart_2: dashboardToTimestamptz(to),
			Limit:         10,
		}); err == nil {
			topUsers = make([]TopUserRow, 0, len(rows))
			for _, row := range rows {
				topUsers = append(topUsers, TopUserRow{
					Email:         row.Email,
					TotalCostUSD:  dashboardNumericToFloat64(row.TotalCostUsd),
					TotalRequests: row.TotalRequests,
				})
			}
		}

		if rows, err := h.querier.GetTopModelsByOrg(r.Context(), db.GetTopModelsByOrgParams{
			OrgID:         org.OrgID,
			WindowStart:   dashboardToTimestamptz(from),
			WindowStart_2: dashboardToTimestamptz(to),
			Limit:         10,
		}); err == nil {
			topModels = make([]TopModelRow, 0, len(rows))
			for _, row := range rows {
				topModels = append(topModels, TopModelRow{
					Model:         row.Model,
					Provider:      row.Provider,
					TotalCostUSD:  dashboardNumericToFloat64(row.TotalCostUsd),
					TotalRequests: row.TotalRequests,
				})
			}
		}
	}

	var buf bytes.Buffer
	if err := templates.Usage(userEmail, orgName, daily, topUsers, topModels).Render(r.Context(), &buf); err != nil {
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
