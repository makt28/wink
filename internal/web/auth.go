package web

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/makt28/wink/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// Session represents an authenticated user session.
type Session struct {
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SessionStore manages in-memory sessions with TTL.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates a session store and starts a background cleanup goroutine.
func NewSessionStore(ttlSeconds int, stopCh <-chan struct{}) *SessionStore {
	ss := &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      time.Duration(ttlSeconds) * time.Second,
	}
	go ss.cleanup(stopCh)
	return ss
}

func (ss *SessionStore) Create(username string) string {
	token := generateToken()
	now := time.Now()
	ss.mu.Lock()
	ss.sessions[token] = &Session{
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(ss.ttl),
	}
	ss.mu.Unlock()
	return token
}

func (ss *SessionStore) Get(token string) *Session {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	s, ok := ss.sessions[token]
	if !ok {
		return nil
	}
	if time.Now().After(s.ExpiresAt) {
		return nil
	}
	return s
}

func (ss *SessionStore) Delete(token string) {
	ss.mu.Lock()
	delete(ss.sessions, token)
	ss.mu.Unlock()
}

func (ss *SessionStore) cleanup(stopCh <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			ss.mu.Lock()
			for token, s := range ss.sessions {
				if now.After(s.ExpiresAt) {
					delete(ss.sessions, token)
				}
			}
			ss.mu.Unlock()
		}
	}
}

// LoginRateLimiter tracks failed login attempts per IP.
type LoginRateLimiter struct {
	mu              sync.Mutex
	attempts        map[string]*loginAttempt
	maxAttempts     int
	lockoutDuration time.Duration
}

type loginAttempt struct {
	failCount int
	lockedAt  time.Time
}

func NewLoginRateLimiter(maxAttempts int, lockoutSeconds int, stopCh <-chan struct{}) *LoginRateLimiter {
	rl := &LoginRateLimiter{
		attempts:        make(map[string]*loginAttempt),
		maxAttempts:     maxAttempts,
		lockoutDuration: time.Duration(lockoutSeconds) * time.Second,
	}
	go rl.cleanup(stopCh)
	return rl
}

// IsLocked returns true if the IP is currently locked out.
func (rl *LoginRateLimiter) IsLocked(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	a, ok := rl.attempts[ip]
	if !ok {
		return false
	}
	if a.failCount >= rl.maxAttempts && time.Since(a.lockedAt) < rl.lockoutDuration {
		return true
	}
	if a.failCount >= rl.maxAttempts && time.Since(a.lockedAt) >= rl.lockoutDuration {
		// Lockout expired, reset
		delete(rl.attempts, ip)
		return false
	}
	return false
}

// RecordFailure increments the failure count for an IP.
func (rl *LoginRateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	a, ok := rl.attempts[ip]
	if !ok {
		a = &loginAttempt{}
		rl.attempts[ip] = a
	}
	a.failCount++
	if a.failCount >= rl.maxAttempts {
		a.lockedAt = time.Now()
	}
}

// ClearIP removes the failure record for an IP on successful login.
func (rl *LoginRateLimiter) ClearIP(ip string) {
	rl.mu.Lock()
	delete(rl.attempts, ip)
	rl.mu.Unlock()
}

func (rl *LoginRateLimiter) cleanup(stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			for ip, a := range rl.attempts {
				if time.Since(a.lockedAt) >= rl.lockoutDuration {
					delete(rl.attempts, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// AuthHandler handles login and logout requests.
type AuthHandler struct {
	cfgMgr   *config.Manager
	sessions *SessionStore
	limiter  *LoginRateLimiter
	tmpl     *TemplateRenderer
}

func NewAuthHandler(cfgMgr *config.Manager, sessions *SessionStore, limiter *LoginRateLimiter, tmpl *TemplateRenderer) *AuthHandler {
	return &AuthHandler{
		cfgMgr:   cfgMgr,
		sessions: sessions,
		limiter:  limiter,
		tmpl:     tmpl,
	}
}

func (ah *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	lang := getLang(r)
	ah.tmpl.Render(w, "login.html", map[string]interface{}{"Lang": lang})
}

func (ah *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr

	if ah.limiter.IsLocked(ip) {
		http.Error(w, "Too many login attempts. Try again later.", http.StatusTooManyRequests)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	cfg := ah.cfgMgr.Get()

	if username != cfg.Auth.Username {
		ah.limiter.RecordFailure(ip)
		slog.Warn("login failed: wrong username", "ip", ip)
		lang := getLang(r)
		ah.tmpl.Render(w, "login.html", map[string]interface{}{"Error": translate(lang, "login.error"), "Lang": lang})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(cfg.Auth.PasswordHash), []byte(password)); err != nil {
		ah.limiter.RecordFailure(ip)
		slog.Warn("login failed: wrong password", "ip", ip)
		lang := getLang(r)
		ah.tmpl.Render(w, "login.html", map[string]interface{}{"Error": translate(lang, "login.error"), "Lang": lang})
		return
	}

	ah.limiter.ClearIP(ip)
	token := ah.sessions.Create(username)

	http.SetCookie(w, &http.Cookie{
		Name:     "wink_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	slog.Info("login successful", "username", username, "ip", ip)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ah *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("wink_session")
	if err == nil {
		ah.sessions.Delete(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "wink_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("failed to generate random token: " + err.Error())
	}
	return hex.EncodeToString(b)
}
