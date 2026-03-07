package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/portal"
	portalauth "github.com/limexpress/gateway/internal/portal/auth"
	"github.com/limexpress/gateway/internal/runtimeconfig"
	"go.uber.org/zap"
)

// setupRefreshTimeout is the maximum time allowed to re-initialize the portal
// auth provider after setup config is saved.
const setupRefreshTimeout = 30 * time.Second

type uiSwitcher struct {
	mu      sync.RWMutex
	current http.Handler

	logger  *zap.Logger
	querier db.Querier
	store   *runtimeconfig.Store
}

func newUISwitcher(ctx context.Context, logger *zap.Logger, pool *pgxpool.Pool, querier db.Querier) *uiSwitcher {
	s := &uiSwitcher{
		logger:  logger,
		querier: querier,
		store:   runtimeconfig.New(pool),
	}
	s.refresh(ctx)
	return s
}

func (s *uiSwitcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.current
	s.mu.RUnlock()
	h.ServeHTTP(w, r)
}

func (s *uiSwitcher) refresh(ctx context.Context) {
	authCfg, missing, err := s.resolvePortalAuthConfig(ctx)
	if err != nil {
		s.logger.Error("failed to resolve runtime auth settings", zap.Error(err))
		s.setHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "failed to load runtime settings", http.StatusInternalServerError)
		}))
		return
	}

	if len(missing) > 0 {
		s.logger.Warn("portal routes disabled: missing runtime settings", zap.Strings("missing", missing))
		s.setHandler(s.newSetupRouter(missing))
		return
	}

	authHandler, err := portalauth.New(ctx, authCfg, s.querier)
	if err != nil {
		s.logger.Error("failed to initialize portal auth", zap.Error(err))
		s.setHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "invalid runtime auth settings", http.StatusInternalServerError)
		}))
		return
	}

	portalRouter := chi.NewRouter()
	portal.New(authHandler, s.querier).RegisterRoutes(portalRouter)
	s.setHandler(portalRouter)
	s.logger.Info("portal routes mounted")
}

func (s *uiSwitcher) setHandler(h http.Handler) {
	s.mu.Lock()
	s.current = h
	s.mu.Unlock()
}

func (s *uiSwitcher) resolvePortalAuthConfig(ctx context.Context) (portalauth.Config, []string, error) {
	get := func(envKey, dbKey string) (string, error) {
		if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
			return v, nil
		}
		v, ok, err := s.store.Get(ctx, dbKey)
		if err != nil {
			return "", err
		}
		if ok {
			return strings.TrimSpace(v), nil
		}
		return "", nil
	}

	clientID, err := get("LIMEXPRESS_OIDC_CLIENT_ID", runtimeconfig.KeyOIDCClientID)
	if err != nil {
		return portalauth.Config{}, nil, err
	}
	clientSecret, err := get("LIMEXPRESS_OIDC_CLIENT_SECRET", runtimeconfig.KeyOIDCClientSecret)
	if err != nil {
		return portalauth.Config{}, nil, err
	}
	redirectURL, err := get("LIMEXPRESS_OIDC_REDIRECT_URL", runtimeconfig.KeyOIDCRedirectURL)
	if err != nil {
		return portalauth.Config{}, nil, err
	}
	sessionSecret, err := s.ensureSessionSecret(ctx)
	if err != nil {
		return portalauth.Config{}, nil, err
	}

	missing := make([]string, 0, 3)
	if clientID == "" {
		missing = append(missing, "LIMEXPRESS_OIDC_CLIENT_ID")
	}
	if clientSecret == "" {
		missing = append(missing, "LIMEXPRESS_OIDC_CLIENT_SECRET")
	}
	if redirectURL == "" {
		missing = append(missing, "LIMEXPRESS_OIDC_REDIRECT_URL")
	}

	return portalauth.Config{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		RedirectURL:   redirectURL,
		SessionSecret: sessionSecret,
	}, missing, nil
}

func (s *uiSwitcher) ensureSessionSecret(ctx context.Context) (string, error) {
	v, ok, err := s.store.Get(ctx, runtimeconfig.KeySessionSecret)
	if err != nil {
		return "", err
	}
	if ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v), nil
	}

	secret, err := generateHexSecret(128)
	if err != nil {
		return "", err
	}
	if err := s.store.SetMany(ctx, map[string]string{
		runtimeconfig.KeySessionSecret: secret,
	}); err != nil {
		return "", err
	}

	s.logger.Info("generated runtime session secret in database")
	return secret, nil
}

func generateHexSecret(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *uiSwitcher) newSetupRouter(initialMissing []string) http.Handler {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		missing := initialMissing
		if _, latestMissing, err := s.resolvePortalAuthConfig(req.Context()); err == nil {
			missing = latestMissing
		}

		msg := ""
		if req.URL.Query().Get("saved") == "1" {
			msg = "Saved. You can continue with /auth/login-page."
		}
		if req.URL.Query().Get("error") != "" {
			msg = req.URL.Query().Get("error")
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderSetupPage(missing, msg)))
	})

	r.Post("/setup/config", func(w http.ResponseWriter, req *http.Request) {
		if !isLocalRequest(req) {
			http.Error(w, "setup endpoint is only accessible from localhost", http.StatusForbidden)
			return
		}

		if err := req.ParseForm(); err != nil {
			http.Redirect(w, req, "/?error="+url.QueryEscape("invalid form"), http.StatusSeeOther)
			return
		}

		clientID := strings.TrimSpace(req.FormValue("oidc_client_id"))
		clientSecret := strings.TrimSpace(req.FormValue("oidc_client_secret"))
		redirectURL := strings.TrimSpace(req.FormValue("oidc_redirect_url"))

		if clientID == "" || clientSecret == "" || redirectURL == "" {
			http.Redirect(w, req, "/?error="+url.QueryEscape("all fields are required"), http.StatusSeeOther)
			return
		}

		parsedURL, err := url.Parse(redirectURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
			http.Redirect(w, req, "/?error="+url.QueryEscape("oidc_redirect_url must be an absolute http or https URL"), http.StatusSeeOther)
			return
		}

		if err := s.store.SetMany(req.Context(), map[string]string{
			runtimeconfig.KeyOIDCClientID:     clientID,
			runtimeconfig.KeyOIDCClientSecret: clientSecret,
			runtimeconfig.KeyOIDCRedirectURL:  redirectURL,
		}); err != nil {
			s.logger.Error("failed to persist runtime settings", zap.Error(err))
			http.Redirect(w, req, "/?error="+url.QueryEscape("failed to save settings"), http.StatusSeeOther)
			return
		}

		refreshCtx, cancel := context.WithTimeout(context.Background(), setupRefreshTimeout)
		defer cancel()
		s.refresh(refreshCtx)
		http.Redirect(w, req, "/?saved=1", http.StatusSeeOther)
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/", http.StatusFound)
	})

	return r
}

func renderSetupPage(missing []string, message string) string {
	missingList := strings.Join(missing, ", ")
	if missingList == "" {
		missingList = "none"
	}

	alert := ""
	if message != "" {
		alert = `<p style="padding:10px;background:#f3f4f6;border:1px solid #d1d5db;border-radius:6px;">` + html.EscapeString(message) + `</p>`
	}

	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>LImExpress Setup</title>
</head>
<body style="font-family: sans-serif; max-width: 760px; margin: 2rem auto; line-height: 1.5;">
  <h1>LImExpress Setup</h1>
  <p>Portal auth settings are not configured yet. Enter the values below to save them in the database.</p>
  <p><strong>Missing:</strong> ` + missingList + `</p>
  ` + alert + `
  <form method="post" action="/setup/config" style="display:grid; gap:12px;">
    <label>OIDC Client ID
      <input type="text" name="oidc_client_id" style="width:100%; padding:8px;" required />
    </label>
    <label>OIDC Client Secret
      <input type="password" name="oidc_client_secret" style="width:100%; padding:8px;" required />
    </label>
    <label>OIDC Redirect URL
      <input type="url" name="oidc_redirect_url" placeholder="http://localhost:8080/auth/callback" style="width:100%; padding:8px;" required />
    </label>
    <button type="submit" style="padding:10px 14px;">Save Configuration</button>
  </form>
  <p style="margin-top:18px; color:#374151;">A random session secret is generated automatically on first boot and stored in the database.</p>
  <p style="margin-top:8px; color:#6b7280; font-size:0.875rem;">The setup endpoint only accepts requests from localhost.</p>
</body>
</html>`
}

// isLocalRequest reports whether the request originated from a loopback address.
// r.RemoteAddr reflects the actual TCP connection and cannot be overridden by
// client-controlled headers, so this check is reliable when the server is not
// behind a proxy. The setup endpoint is intentionally localhost-only.
func isLocalRequest(r *http.Request) bool {
	host := r.RemoteAddr
	// Strip port if present.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
