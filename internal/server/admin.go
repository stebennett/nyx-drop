package server

import (
	"bytes"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nyx-drop/internal/auth"
	"nyx-drop/internal/clock"
	"nyx-drop/web"
)

const (
	stateCookie   = "oauth-state"
	sessionCookie = "session"
	stateTTL      = 10 * time.Minute
	sessionTTL    = 7 * 24 * time.Hour
)

type adminAuth struct {
	clock        clock.Clock
	log          *slog.Logger
	adminUser    string
	signer       *auth.Signer
	github       *auth.GitHub
	origin       string
	secure       bool
	unauthorized *template.Template
}

func registerAdminRoutes(apex *http.ServeMux, d Deps) error {
	unauthorized, err := template.ParseFS(web.Files, "unauthorized.html")
	if err != nil {
		return err
	}
	origin := d.Config.ExternalOrigin()
	a := &adminAuth{
		clock:        d.Clock,
		log:          d.Logger,
		adminUser:    d.Config.AdminGitHubUser,
		signer:       auth.NewSigner(d.Config.SessionSecret),
		github:       auth.NewGitHub(d.Config.GitHubClientID, d.Config.GitHubClientSecret, origin+"/auth/callback", d.GitHubEndpoints),
		origin:       origin,
		secure:       d.Config.Scheme != "http",
		unauthorized: unauthorized,
	}

	apex.HandleFunc("GET /auth/login", a.login)
	apex.HandleFunc("GET /auth/callback", a.callback)
	// ServeMux also matches HEAD to GET by default; don't issue state,
	// clear it, or exchange an authorization code on a HEAD request.
	for _, path := range []string{"/auth/login", "/auth/callback"} {
		apex.HandleFunc("HEAD "+path, authGetOnly)
	}
	apex.Handle("POST /auth/logout", a.guard(false, http.HandlerFunc(a.logout)))

	// Register future admin pages/actions on these sub-muxes, not the apex
	// mux, so new method-specific patterns inherit both auth checks.
	pages := http.NewServeMux()
	pages.HandleFunc("GET /admin", adminPlaceholder)
	api := http.NewServeMux()
	api.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeAdminError(w, http.StatusNotFound, "not found")
	})
	pageHandler := a.guard(true, pages)
	apex.Handle("/admin", pageHandler)
	apex.Handle("/admin/", pageHandler)
	apiHandler := a.guard(false, api)
	apex.Handle("/api/admin", apiHandler)
	apex.Handle("/api/admin/", apiHandler)
	return nil
}

func authGetOnly(w http.ResponseWriter, _ *http.Request) {
	authResponseHeaders(w)
	w.Header().Set("Allow", "GET")
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (a *adminAuth) login(w http.ResponseWriter, r *http.Request) {
	authResponseHeaders(w)
	state, verifier := auth.NewState()
	value := a.signer.SignState(state, verifier, a.clock.Now().Add(stateTTL))
	http.SetCookie(w, a.cookie(stateCookie, value, stateTTL))
	http.Redirect(w, r, a.github.AuthorizeURL(state, verifier), http.StatusFound)
}

func (a *adminAuth) callback(w http.ResponseWriter, r *http.Request) {
	authResponseHeaders(w)
	// Consume the browser's state on any callback outcome. This isn't a
	// server-side replay cache; GitHub enforces single-use of the code.
	http.SetCookie(w, a.cookie(stateCookie, "", 0))
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || q.Has("error") || len(q["state"]) != 1 || len(q["code"]) != 1 || q.Get("code") == "" {
		http.Error(w, "Invalid sign-in callback", http.StatusForbidden)
		return
	}
	verifier, valid := a.signer.VerifyState(a.readCookie(r, stateCookie), q.Get("state"), a.clock.Now())
	if !valid {
		http.Error(w, "Invalid sign-in callback", http.StatusForbidden)
		return
	}
	login, err := a.github.ExchangeLogin(r.Context(), q.Get("code"), verifier)
	if err != nil {
		http.Error(w, "GitHub sign-in failed", http.StatusBadGateway)
		return
	}
	if !a.isAdmin(login) {
		var body bytes.Buffer
		if err := a.unauthorized.Execute(&body, login); err != nil {
			http.Error(w, "Could not render sign-in result", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		w.Write(body.Bytes())
		return
	}
	subject := strings.ToLower(login)
	value := a.signer.SignSession(subject, a.clock.Now().Add(sessionTTL))
	http.SetCookie(w, a.cookie(sessionCookie, value, sessionTTL))
	a.log.Info("admin login", "user", subject)
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *adminAuth) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(sessionCookie, "", 0))
	http.Redirect(w, r, "/", http.StatusFound)
}

func adminPlaceholder(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Nyx Drop Admin</title><h1>Nyx Drop Admin</h1><form method="post" action="/auth/logout"><button>Sign out</button></form></html>`))
}

func (a *adminAuth) isAdmin(login string) bool {
	return login != "" && strings.EqualFold(login, a.adminUser)
}

func (a *adminAuth) cookieName(name string) string {
	if a.secure {
		return "__Host-" + name
	}
	return name
}

func (a *adminAuth) readCookie(r *http.Request, name string) string {
	cookies := r.CookiesNamed(a.cookieName(name))
	if len(cookies) != 1 {
		return ""
	}
	return cookies[0].Value
}

func (a *adminAuth) cookie(name, value string, ttl time.Duration) *http.Cookie {
	c := &http.Cookie{
		Name:     a.cookieName(name),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl / time.Second),
	}
	if ttl == 0 {
		c.MaxAge = -1 // serializes as Max-Age=0
		c.Expires = time.Unix(0, 0).UTC()
	}
	return c
}
