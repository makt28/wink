package web

import (
	"net/http"

	"github.com/makt28/wink/internal/config"
)

// AuthMiddleware checks for SSO header or a valid session cookie on protected routes.
func AuthMiddleware(sessions *SessionStore, cfgMgr *config.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check SSO header first (trusts reverse proxy Remote-User header)
			cfg := cfgMgr.Get()
			if cfg.Auth.SSO.Enabled {
				if user := r.Header.Get("Remote-User"); user != "" {
					next.ServeHTTP(w, r)
					return
				}
			}

			cookie, err := r.Cookie("wink_session")
			if err != nil {
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
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
