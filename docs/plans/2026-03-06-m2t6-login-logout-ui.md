# M2-T6 Login / Logout UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire the login/logout flow end-to-end: auth routes registered, portal routes protected, real org name in nav, access-denied page, and logout flash.

**Architecture:** The backend auth handlers (OIDC, session, org middleware) already exist in `internal/portal/auth/`. This plan wires them into the router via `handlers.go` and `main.go`, updates templates to accept live data (org name, signed-out flash), adds a new access-denied page, and covers the changes with unit tests.

**Tech Stack:** Go 1.25, chi v5, templ v0.3.1001, daisyUI 4, HTMX 1.9, gorilla/sessions

---

## Context

### What already exists (DO NOT re-implement)
- `internal/portal/auth/oidc.go` — `LoginHandler`, `CallbackHandler`, `LogoutHandler`, `RequireAuth`
- `internal/portal/auth/orgcontext.go` — `OrgMiddleware`, `SwitchOrgHandler`
- `internal/portal/auth/context.go` — `UserContext`, `UserFromContext`
- `internal/portal/templates/login.templ` — login page with Google button (just needs `signedOut bool` param added)
- `internal/portal/templates/nav.templ` — nav with logout button (just needs `orgName string` param)
- `internal/portal/templates/layout.templ` — base layout (needs `orgName` threaded through)
- `internal/portal/templates/index.templ` — dashboard (needs `orgName string` param)
- `internal/portal/templates/flash.templ` — flash component (no changes)
- `internal/config/config.go` — `OIDCConfig`, `SessionConfig` already wired

### What is missing (this plan's work)
1. `access_denied.templ` — new page for users with no org membership
2. Template signature updates — `orgName` and `signedOut bool` params
3. `handlers.go` — accept auth handler, register all routes with middleware
4. `auth/oidc.go` — change `LogoutHandler` redirect to show signed-out flash
5. `main.go` — instantiate `auth.Handler` and register portal routes
6. `handlers_test.go` — route protection and login page tests

### Route map after this plan
| Method | Path | Handler | Middleware |
|--------|------|---------|-----------|
| GET | `/auth/login-page` | `loginPageHandler` | — |
| GET | `/auth/login` | `auth.LoginHandler` | — |
| GET | `/auth/callback` | `auth.CallbackHandler` | — |
| POST | `/auth/logout` | `auth.LogoutHandler` | — |
| GET | `/portal` | `indexHandler` | `RequireAuth` → `OrgMiddleware` |
| GET | `/portal/access-denied` | `accessDeniedHandler` | `RequireAuth` |
| POST | `/portal/switch-org` | `auth.SwitchOrgHandler` | `RequireAuth` → `OrgMiddleware` |
| GET | `/` | redirect to `/auth/login-page` | — |

### templ generate command
```bash
go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate ./internal/portal/templates/
```

---

## Task 1: Update `login.templ` to support signed-out flash

**Files:**
- Modify: `internal/portal/templates/login.templ`

**Step 1: Edit the templ source**

Replace the signature and add a flash banner above the sign-in card when `signedOut` is true:

```templ
package templates

// Login renders the sign-in page for unauthenticated users.
// signedOut is true when the user has just logged out (shows a flash banner).
templ Login(signedOut bool) {
	@Base("Sign in", "", "") {
		<div class="hero min-h-[70vh]">
			<div class="hero-content flex-col w-full max-w-sm">
				<!-- Logo / branding -->
				<div class="text-center mb-2">
					<h1 class="text-3xl font-bold text-base-content">LImExpress</h1>
					<p class="text-base-content/60 mt-1 text-sm">LLM API gateway for teams</p>
				</div>
				if signedOut {
					@Flash("success", "You have been signed out.")
				}
				<!-- Sign-in card -->
				<div class="card bg-base-100 shadow-xl w-full">
					<div class="card-body gap-6">
						<div class="text-center">
							<h2 class="text-xl font-semibold text-base-content">Sign in to your account</h2>
							<p class="text-base-content/60 text-sm mt-1">Use your Google Workspace account</p>
						</div>
						<!-- Google sign-in button with brand styling -->
						<a
							href="/auth/login"
							class="flex items-center justify-center gap-3 w-full border border-[#dadce0] rounded-lg px-4 py-2.5 bg-white hover:bg-gray-50 transition-colors duration-150 shadow-sm"
						>
							<!-- Google logo SVG (official G icon) -->
							<svg width="18" height="18" viewBox="0 0 18 18" xmlns="http://www.w3.org/2000/svg">
								<path d="M17.64 9.205c0-.639-.057-1.252-.164-1.841H9v3.481h4.844a4.14 4.14 0 0 1-1.796 2.716v2.259h2.908c1.702-1.567 2.684-3.875 2.684-6.615z" fill="#4285F4"></path>
								<path d="M9 18c2.43 0 4.467-.806 5.956-2.18l-2.908-2.259c-.806.54-1.837.86-3.048.86-2.344 0-4.328-1.584-5.036-3.711H.957v2.332A8.997 8.997 0 0 0 9 18z" fill="#34A853"></path>
								<path d="M3.964 10.71A5.41 5.41 0 0 1 3.682 9c0-.593.102-1.17.282-1.71V4.958H.957A8.996 8.996 0 0 0 0 9c0 1.452.348 2.827.957 4.042l3.007-2.332z" fill="#FBBC05"></path>
								<path d="M9 3.58c1.321 0 2.508.454 3.44 1.345l2.582-2.58C13.463.891 11.426 0 9 0A8.997 8.997 0 0 0 .957 4.958L3.964 7.29C4.672 5.163 6.656 3.58 9 3.58z" fill="#EA4335"></path>
							</svg>
							<span class="text-sm font-medium text-[#3c4043]">Sign in with Google</span>
						</a>
						<p class="text-center text-xs text-base-content/40">
							By signing in you agree to your organisation's access policy.
						</p>
					</div>
				</div>
			</div>
		</div>
	}
}
```

**Step 2: Update `nav.templ` to accept `orgName string`**

Replace `Nav(userEmail string)` with `Nav(userEmail string, orgName string)`:

```templ
package templates

// Nav renders the top navigation bar.
// userEmail is empty when unauthenticated. orgName is the active organisation name.
templ Nav(userEmail string, orgName string) {
	<nav class="navbar bg-base-100 shadow-lg px-4">
		<div class="navbar-start">
			<div class="dropdown lg:hidden">
				<label tabindex="0" class="btn btn-ghost btn-circle">
					<svg xmlns="http://www.w3.org/2000/svg" class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"></path>
					</svg>
				</label>
				<ul tabindex="0" class="menu menu-sm dropdown-content mt-3 z-[1] p-2 shadow bg-base-100 rounded-box w-52">
					<li><a href="/portal">Dashboard</a></li>
					<li><a href="/portal/keys">API Keys</a></li>
					<li><a href="/portal/usage">Usage</a></li>
				</ul>
			</div>
			<a href="/portal" class="btn btn-ghost text-xl font-bold">LImExpress</a>
		</div>
		<div class="navbar-center hidden lg:flex">
			<ul class="menu menu-horizontal px-1 gap-1">
				<li><a href="/portal" class="rounded-lg">Dashboard</a></li>
				<li><a href="/portal/keys" class="rounded-lg">API Keys</a></li>
				<li><a href="/portal/usage" class="rounded-lg">Usage</a></li>
			</ul>
		</div>
		<div class="navbar-end gap-2">
			if userEmail != "" {
				<!-- Org switcher (real org name from OrgContext) -->
				<div class="dropdown dropdown-end">
					<label tabindex="0" class="btn btn-ghost btn-sm normal-case font-medium">
						{ orgName }
						<svg xmlns="http://www.w3.org/2000/svg" class="h-4 w-4 ml-1" fill="none" viewBox="0 0 24 24" stroke="currentColor">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
						</svg>
					</label>
					<ul tabindex="0" class="dropdown-content menu p-2 shadow bg-base-100 rounded-box w-52 z-[1] mt-1">
						<li class="menu-title"><span>Switch Organisation</span></li>
						<li><a class="active">{ orgName }</a></li>
						<li><a>— more coming soon —</a></li>
					</ul>
				</div>
				<!-- User avatar + sign-out -->
				<div class="dropdown dropdown-end">
					<label tabindex="0" class="btn btn-ghost btn-circle avatar placeholder">
						<div class="bg-primary text-primary-content rounded-full w-8">
							<span class="text-xs">{ string([]rune(userEmail)[:1]) }</span>
						</div>
					</label>
					<ul tabindex="0" class="dropdown-content menu p-2 shadow bg-base-100 rounded-box w-56 z-[1] mt-1">
						<li class="menu-title"><span class="text-xs truncate">{ userEmail }</span></li>
						<li>
							<form method="POST" action="/auth/logout">
								<button type="submit" class="w-full text-left">Sign out</button>
							</form>
						</li>
					</ul>
				</div>
			} else {
				<a href="/auth/login" class="btn btn-primary btn-sm">Sign in</a>
			}
		</div>
	</nav>
}
```

**Step 3: Update `layout.templ` to thread `orgName` to Nav**

```templ
package templates

// Base is the root layout used by every portal page.
// It loads Tailwind + daisyUI and HTMX from CDN (acceptable for MVP).
templ Base(title string, user string, orgName string) {
	<!DOCTYPE html>
	<html lang="en" data-theme="corporate">
		<head>
			<meta charset="UTF-8"/>
			<meta name="viewport" content="width=device-width, initial-scale=1.0"/>
			<title>{ title } — LImExpress</title>
			<!-- Tailwind + daisyUI via CDN for MVP -->
			<link href="https://cdn.jsdelivr.net/npm/daisyui@4/dist/full.min.css" rel="stylesheet"/>
			<script src="https://cdn.tailwindcss.com"></script>
			<!-- HTMX -->
			<script src="https://unpkg.com/htmx.org@1.9.12"></script>
		</head>
		<body class="min-h-screen bg-base-200">
			@Nav(user, orgName)
			<div id="flash-region" class="container mx-auto px-4 pt-4 max-w-7xl"></div>
			<main class="container mx-auto px-4 py-6 max-w-7xl">
				{ children... }
			</main>
		</body>
	</html>
}
```

**Step 4: Update `index.templ` to accept `orgName string`**

```templ
package templates

// Index is the portal home/dashboard shown after login.
templ Index(userEmail string, orgName string) {
	@Base("Dashboard", userEmail, orgName) {
		<div class="mb-6">
			<h1 class="text-2xl font-bold text-base-content">Dashboard</h1>
			<p class="text-base-content/60 text-sm mt-1">Overview of your LImExpress usage</p>
		</div>
		<div class="stats stats-vertical lg:stats-horizontal shadow w-full mb-8 bg-base-100">
			@StatCard("API Keys", "—", "key")
			@StatCard("Today's Cost", "$—", "chart-bar")
			@StatCard("Requests Today", "—", "arrow-path")
		</div>
		<div class="card bg-base-100 shadow">
			<div class="card-body">
				<h2 class="card-title text-lg">Usage — last 30 days</h2>
				<p class="text-base-content/60 text-sm mb-4">Data will appear here once you start making requests.</p>
				<div class="w-full h-40 bg-base-200 rounded-lg flex items-center justify-center">
					<span class="text-base-content/30 text-sm">Chart coming soon</span>
				</div>
			</div>
		</div>
	}
}

// StatCard renders a single daisyUI stat child element.
// Must be placed inside a <div class="stats"> wrapper (see Index above).
templ StatCard(label string, value string, icon string) {
	<div class="stat">
		<div class="stat-title">{ label }</div>
		<div class="stat-value text-primary">{ value }</div>
		<div class="stat-desc">Updated just now</div>
	</div>
}
```

**Step 5: Run templ generate**

```bash
go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate ./internal/portal/templates/
```

Expected: generates updated `*_templ.go` files with no errors.

**Step 6: Verify compilation**

```bash
go build ./...
```

Expected: FAIL (because `handlers.go` still calls `Login()` and `Index(email)` with old signatures). This is expected — the handlers will be fixed in Task 3.

---

## Task 2: Create `access_denied.templ`

**Files:**
- Create: `internal/portal/templates/access_denied.templ`

**Step 1: Create the templ file**

```templ
package templates

// AccessDenied renders the access-denied page shown when a user has no org memberships.
// userEmail is used to populate the nav (user is authenticated, just has no org).
templ AccessDenied(userEmail string) {
	@Base("Access Denied", userEmail, "") {
		<div class="hero min-h-[60vh]">
			<div class="hero-content text-center flex-col max-w-md">
				<!-- Icon -->
				<div class="mb-4">
					<div class="w-16 h-16 rounded-full bg-warning/20 flex items-center justify-center mx-auto">
						<svg xmlns="http://www.w3.org/2000/svg" class="h-8 w-8 text-warning" fill="none" viewBox="0 0 24 24" stroke="currentColor">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
						</svg>
					</div>
				</div>
				<h1 class="text-2xl font-bold text-base-content">No organisation access</h1>
				<p class="text-base-content/60 mt-2">
					Your account (<span class="font-medium text-base-content">{ userEmail }</span>) is not a member of any organisation.
					Contact your administrator to request access.
				</p>
				<div class="mt-6 flex gap-3 justify-center">
					<form method="POST" action="/auth/logout">
						<button type="submit" class="btn btn-outline btn-sm">Sign out</button>
					</form>
				</div>
			</div>
		</div>
	}
}
```

**Step 2: Run templ generate**

```bash
go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate ./internal/portal/templates/
```

Expected: generates `access_denied_templ.go` with no errors.

---

## Task 3: Update `handlers.go` with full route wiring

**Files:**
- Modify: `internal/portal/handlers.go`

**Step 1: Write the new handlers.go**

```go
package portal

import (
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
		r.Post("/portal/switch-org", h.authHandler.SwitchOrgHandler(h.querier).ServeHTTP)
	})
}

// loginPageHandler renders the sign-in page.
// Reads ?signed_out=1 to show a "signed out" success flash.
func (h *Handler) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	signedOut := r.URL.Query().Get("signed_out") == "1"
	component := templates.Login(signedOut)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
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
	component := templates.Index(userEmail, orgName)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}

// accessDeniedHandler renders the access-denied page for authenticated users
// who have no org memberships.
func (h *Handler) accessDeniedHandler(w http.ResponseWriter, r *http.Request) {
	userEmail := ""
	if u, ok := auth.UserFromContext(r.Context()); ok {
		userEmail = u.Email
	}
	component := templates.AccessDenied(userEmail)
	if err := component.Render(r.Context(), w); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
```

**Step 2: Verify compilation**

```bash
go build ./internal/portal/...
```

Expected: PASS.

---

## Task 4: Update `LogoutHandler` to redirect to login page

**Files:**
- Modify: `internal/portal/auth/oidc.go:182-189`

**Step 1: Change the redirect target**

Find this in `oidc.go`:
```go
// LogoutHandler clears the session cookie and redirects to /.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, sessionName)
	if err == nil {
		sess.Options.MaxAge = -1
		_ = sess.Save(r, w)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
```

Replace with:
```go
// LogoutHandler clears the session cookie and redirects to the login page.
// The ?signed_out=1 param causes loginPageHandler to show a "signed out" flash.
func (h *Handler) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	sess, err := h.store.Get(r, sessionName)
	if err == nil {
		sess.Options.MaxAge = -1
		_ = sess.Save(r, w)
	}
	http.Redirect(w, r, "/auth/login-page?signed_out=1", http.StatusFound)
}
```

**Step 2: Verify compilation**

```bash
go build ./internal/portal/...
```

Expected: PASS.

---

## Task 5: Update `main.go` to wire auth and portal routes

**Files:**
- Modify: `cmd/gateway/main.go`

**Step 1: Write the updated main.go**

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/metrics"
	"github.com/limexpress/gateway/internal/portal"
	portalauth "github.com/limexpress/gateway/internal/portal/auth"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}

	logger, err := metrics.New(cfg.Log.Level)
	if err != nil {
		panic(fmt.Sprintf("failed to initialise logger: %v", err))
	}
	defer logger.Sync() //nolint:errcheck

	cfg.LogSummary(logger)

	// Initialize DB pool.
	pool, err := db.NewPool(ctx, cfg.DB.DSN)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	querier := db.New(pool)

	r := chi.NewRouter()
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Portal routes — only mounted when OIDC is configured.
	if cfg.OIDC.ClientID != "" && cfg.Session.Secret != "" {
		authHandler, err := portalauth.New(ctx, portalauth.Config{
			ClientID:      cfg.OIDC.ClientID,
			ClientSecret:  cfg.OIDC.ClientSecret,
			RedirectURL:   cfg.OIDC.RedirectURL,
			SessionSecret: cfg.Session.Secret,
		}, querier)
		if err != nil {
			logger.Fatal("failed to initialize OIDC handler", zap.Error(err))
		}
		portalHandler := portal.New(authHandler, querier)
		portalHandler.RegisterRoutes(r)
		logger.Info("portal routes mounted")
	} else {
		logger.Warn("OIDC not configured — portal routes disabled",
			zap.Bool("oidc_client_id_set", cfg.OIDC.ClientID != ""),
			zap.Bool("session_secret_set", cfg.Session.Secret != ""),
		)
	}

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("gateway starting",
		zap.String("addr", addr),
		zap.String("log_level", cfg.Log.Level),
	)

	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Fatal("server error", zap.Error(err))
	}
}
```

**Step 2: Verify compilation**

```bash
go build ./...
```

Expected: PASS.

---

## Task 6: Add tests for portal handlers

**Files:**
- Create: `internal/portal/handlers_test.go`

**Step 1: Write the test file**

```go
package portal_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/limexpress/gateway/internal/portal"
	portalauth "github.com/limexpress/gateway/internal/portal/auth"
)

// newTestRouter builds a chi router with portal routes wired up using a
// test-only auth handler (no real OIDC provider — session validation only).
func newTestRouter() *chi.Mux {
	store := sessions.NewCookieStore([]byte("test-secret-32bytes-for-testing!"))
	authHandler := portalauth.NewWithStore(store, nil)
	h := portal.New(authHandler, nil)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// TestLoginPage_Renders verifies that GET /auth/login-page returns 200.
func TestLoginPage_Renders(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/login-page", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body")
	}
}

// TestLoginPage_SignedOutFlash verifies that ?signed_out=1 causes the page to
// include the "signed out" flash message.
func TestLoginPage_SignedOutFlash(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/auth/login-page?signed_out=1", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body == "" {
		t.Fatal("expected non-empty body")
	}
	// The flash text must appear in the rendered HTML.
	if !contains(body, "signed out") {
		t.Errorf("expected 'signed out' text in response body, got:\n%s", body)
	}
}

// TestRootRedirect verifies that GET / redirects to /auth/login-page.
func TestRootRedirect(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login-page" {
		t.Fatalf("expected redirect to /auth/login-page, got %q", loc)
	}
}

// TestPortalDashboard_Unauthenticated verifies GET /portal redirects to /auth/login
// when there is no valid session.
func TestPortalDashboard_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/portal", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

// TestAccessDenied_Unauthenticated verifies GET /portal/access-denied redirects
// to /auth/login when there is no valid session.
func TestAccessDenied_Unauthenticated(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest(http.MethodGet, "/portal/access-denied", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected %d, got %d", http.StatusFound, rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/auth/login" {
		t.Fatalf("expected redirect to /auth/login, got %q", loc)
	}
}

// contains is a helper to check if s contains substr (case-insensitive simple check).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

Note: The tests require `portalauth.NewWithStore(store, nil)` — a test-only constructor that accepts an existing store. Add it in **Step 2**.

**Step 2: Add `NewWithStore` to `auth/oidc.go`**

Add after the `New` function in `internal/portal/auth/oidc.go`:

```go
// NewWithStore creates a Handler with a pre-configured session store.
// Only used in tests where a real OIDC provider is not available.
func NewWithStore(store *sessions.CookieStore, querier db.Querier) *Handler {
	return &Handler{store: store, db: querier}
}
```

**Step 3: Run tests**

```bash
go test ./internal/portal/... -v -run TestLoginPage -run TestRootRedirect -run TestPortalDashboard -run TestAccessDenied
```

Expected: all 5 tests PASS.

**Step 4: Run all tests to confirm nothing is broken**

```bash
go test ./...
```

Expected: PASS (all existing tests still pass).

---

## Task 7: Final verification and commit

**Step 1: Build everything**

```bash
go build ./...
```

Expected: PASS with no errors.

**Step 2: Run full test suite**

```bash
go test ./...
```

Expected: PASS.

**Step 3: Verify templ generated files are up to date**

```bash
go run github.com/a-h/templ/cmd/templ@v0.3.1001 generate ./internal/portal/templates/
go build ./...
```

Expected: no new changes (generate is idempotent when source matches).

**Step 4: Commit**

```bash
git add \
  internal/portal/templates/login.templ \
  internal/portal/templates/login_templ.go \
  internal/portal/templates/nav.templ \
  internal/portal/templates/nav_templ.go \
  internal/portal/templates/layout.templ \
  internal/portal/templates/layout_templ.go \
  internal/portal/templates/index.templ \
  internal/portal/templates/index_templ.go \
  internal/portal/templates/access_denied.templ \
  internal/portal/templates/access_denied_templ.go \
  internal/portal/handlers.go \
  internal/portal/handlers_test.go \
  internal/portal/auth/oidc.go \
  cmd/gateway/main.go

git commit -m "feat(portal): M2-T6 login/logout UI — wire auth routes, org context in nav, access-denied page"
```

---

## How to test end-to-end

Set environment variables:
```bash
export LIMEXPRESS_OIDC_CLIENT_ID=<google-client-id>
export LIMEXPRESS_OIDC_CLIENT_SECRET=<google-client-secret>
export LIMEXPRESS_OIDC_REDIRECT_URL=http://localhost:8080/auth/callback
export LIMEXPRESS_SESSION_SECRET=<64-hex-chars>
export DB_DSN=postgres://...
```

Then:
1. `go run ./cmd/gateway/` — server starts, logs "portal routes mounted"
2. Visit `http://localhost:8080/` → redirects to `/auth/login-page`
3. Click "Sign in with Google" → redirects to Google OAuth
4. After Google auth → callback → `/portal` dashboard with real org name in nav
5. Click avatar → "Sign out" → session cleared → `/auth/login-page?signed_out=1` → green "You have been signed out." flash
6. Visit `http://localhost:8080/portal` without session → redirects to `/auth/login`
