package web

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/makt28/wink/internal/config"
	"github.com/makt28/wink/internal/storage"
	webassets "github.com/makt28/wink/web"
)

// i18n translations: lang -> key -> text
var translations map[string]map[string]string

func init() {
	translations = make(map[string]map[string]string)
	for _, lang := range []string{"en", "zh"} {
		data, err := webassets.I18nFS.ReadFile("i18n/" + lang + ".json")
		if err != nil {
			panic("failed to load i18n/" + lang + ".json: " + err.Error())
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			panic("failed to parse i18n/" + lang + ".json: " + err.Error())
		}
		translations[lang] = m
	}
}

// translate looks up a key for the given language.
func translate(lang, key string) string {
	if m, ok := translations[lang]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	// Fallback to English
	if m, ok := translations["en"]; ok {
		if v, ok := m[key]; ok {
			return v
		}
	}
	return key
}

// jsI18nKeys are the translation keys needed by client-side JavaScript.
var jsI18nKeys = []string{
	"dash.status_up", "dash.status_down", "dash.status_unknown",
	"dash.no_monitors", "dash.add_first",
	"dash.no_incidents", "dash.ongoing", "dash.last_check",
	"dash.duration", "dash.response_time", "dash.heartbeat",
	"dash.incidents", "dash.select_monitor", "dash.back",
	"dash.edit", "dash.clone", "dash.delete", "dash.delete_confirm",
	"dash.type", "dash.interval",
	"dash.pause", "dash.resume", "dash.status_paused",
	"dash.ungrouped",
	"settings.test_success", "settings.test_failed",
	"settings.no_chats_found",
}

// buildJSI18n returns a map of translation keys needed by JavaScript.
func buildJSI18n(lang string) map[string]string {
	m := make(map[string]string, len(jsI18nKeys))
	for _, k := range jsI18nKeys {
		m[k] = translate(lang, k)
	}
	return m
}

// TemplateRenderer parses each page template paired with layout.html.
type TemplateRenderer struct {
	templates map[string]*template.Template
}

func NewTemplateRenderer() *TemplateRenderer {
	tmplFS, err := fs.Sub(webassets.TemplatesFS, "templates")
	if err != nil {
		slog.Error("failed to access templates", "error", err)
		panic(err)
	}

	funcMap := template.FuncMap{
		"t": func(lang, key string) string {
			return translate(lang, key)
		},
		"toJSON": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
	}

	pages := []string{"dashboard.html", "monitor_form.html", "settings.html", "groups.html"}
	templates := make(map[string]*template.Template)

	for _, page := range pages {
		tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(tmplFS, "layout.html", page))
		templates[page] = tmpl
	}

	// login.html is standalone
	templates["login.html"] = template.Must(template.New("").Funcs(funcMap).ParseFS(tmplFS, "login.html"))

	return &TemplateRenderer{templates: templates}
}

func (tr *TemplateRenderer) Render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tmpl, ok := tr.templates[name]
	if !ok {
		slog.Error("template not found", "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	execName := name
	if name != "login.html" {
		execName = "layout"
	}

	if err := tmpl.ExecuteTemplate(w, execName, data); err != nil {
		slog.Error("template render error", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// getLang reads language preference from cookie, default "en".
func getLang(r *http.Request) string {
	c, err := r.Cookie("wink_lang")
	if err == nil && (c.Value == "zh" || c.Value == "en") {
		return c.Value
	}
	return "en"
}

// getTheme reads theme preference from cookie, default "light".
func getTheme(r *http.Request) string {
	c, err := r.Cookie("wink_theme")
	if err == nil && c.Value == "dark" {
		return "dark"
	}
	return "light"
}

// NewRouter sets up all routes and returns the http.Handler.
func NewRouter(cfgMgr *config.Manager, histMgr *storage.HistoryManager, stopCh <-chan struct{}) http.Handler {
	cfg := cfgMgr.Get()
	r := chi.NewRouter()

	tmpl := NewTemplateRenderer()

	sessions := NewSessionStore(cfg.System.SessionTTL, stopCh)
	limiter := NewLoginRateLimiter(cfg.Auth.MaxLoginAttempts, cfg.Auth.LockoutDuration, stopCh)

	auth := NewAuthHandler(cfgMgr, sessions, limiter, tmpl)
	handlers := NewHandlers(cfgMgr, histMgr, tmpl)
	health := NewHealthHandler(cfgMgr)

	staticSub, err := fs.Sub(webassets.StaticFS, "static")
	if err != nil {
		panic(err)
	}

	// Language switch
	r.Get("/lang", func(w http.ResponseWriter, r *http.Request) {
		lang := r.URL.Query().Get("l")
		if lang != "zh" {
			lang = "en"
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "wink_lang",
			Value:    lang,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   365 * 24 * 3600,
		})
		ref := r.Header.Get("Referer")
		if ref == "" {
			ref = "/"
		}
		http.Redirect(w, r, ref, http.StatusSeeOther)
	})

	// Theme switch
	r.Get("/theme", func(w http.ResponseWriter, r *http.Request) {
		theme := r.URL.Query().Get("t")
		if theme != "dark" {
			theme = "light"
		}
		http.SetCookie(w, &http.Cookie{
			Name:   "wink_theme",
			Value:  theme,
			Path:   "/",
			MaxAge: 365 * 24 * 3600,
		})
		w.WriteHeader(http.StatusNoContent)
	})

	// Public routes
	r.Get("/login", auth.LoginPage)
	r.Post("/login", auth.Login)
	r.Get("/healthz", health.ServeHTTP)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(sessions, cfgMgr))

		r.Get("/", handlers.Dashboard)
		r.Get("/monitors/new", handlers.MonitorForm)
		r.Post("/monitors", handlers.CreateMonitor)
		r.Get("/monitors/{id}/edit", handlers.EditMonitorForm)
		r.Get("/monitors/{id}/clone", handlers.CloneMonitorForm)
		r.Post("/monitors/{id}", handlers.UpdateMonitor)
		r.Post("/monitors/delete", handlers.DeleteMonitor)

		// JSON API endpoints
		r.Get("/api/monitors", handlers.APIMonitors)
		r.Get("/api/monitors/{id}", handlers.APIMonitorDetail)
		r.Post("/api/monitors/{id}/toggle", handlers.ToggleMonitor)

		r.Get("/groups", handlers.GroupsPage)
		r.Get("/settings", handlers.SettingsPage)
		r.Post("/settings/system", handlers.SaveSystem)
		r.Post("/settings/auth", handlers.SaveAuth)
		r.Post("/settings/sso", handlers.SaveSSO)
		r.Post("/settings/groups", handlers.CreateGroup)
		r.Post("/settings/groups/delete", handlers.DeleteGroup)
		r.Post("/settings/groups/rename", handlers.RenameGroup)
		r.Post("/settings/notifiers", handlers.AddNotifierFlat)
		r.Post("/settings/notifiers/update", handlers.UpdateNotifier)
		r.Post("/settings/notifiers/delete", handlers.DeleteNotifierByID)
		r.Post("/api/notifiers/{id}/test", handlers.TestNotifier)
		r.Post("/api/telegram/get-updates", handlers.TelegramGetUpdates)
		r.Get("/api/check-update", handlers.CheckUpdate)

		r.Post("/logout", auth.Logout)
	})

	return r
}
