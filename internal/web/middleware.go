package web

import (
	"net/http"
	"strings"

	"github.com/makt28/wink/internal/config"
)

// AuthMiddleware checks for SSO header or a valid session cookie on protected routes.
func AuthMiddleware(sessions *SessionStore, cfgMgr *config.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check SSO header first, but only trust loopback proxy sources.
			cfg := cfgMgr.Get()
			if cfg.Auth.SSO.Enabled && isTrustedSSOSource(r.RemoteAddr) {
				if user := strings.TrimSpace(r.Header.Get("Remote-User")); user != "" {
					next.ServeHTTP(w, r)
					return
				}
			}

			isAPI := r.Header.Get("X-Requested-With") == "XMLHttpRequest" || strings.HasPrefix(r.URL.Path, "/api/")

			cookie, err := r.Cookie("wink_session")
			if err != nil {
				if isAPI {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			session := sessions.Get(cookie.Value)
			if session == nil {
				// Expired or invalid session, clear cookie
				http.SetCookie(w, &http.Cookie{
					Name:     "wink_session",
					Value:    "",
					Path:     "/",
					MaxAge:   -1,
					HttpOnly: true,
				})
				if isAPI {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
