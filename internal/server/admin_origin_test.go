package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nyx-drop/internal/auth"
)

func TestAdminMutationsCheckConfiguredOrigin(t *testing.T) {
	const sibling = "https://uploaded." + testBaseDomain + ":9443"
	const backend = "https://" + testBaseDomain + ":8080"
	h, fc, origin := authFixture(t, "https", nil, http.NotFound)
	session := &http.Cookie{Name: "__Host-session", Value: auth.NewSigner(testSessionKey).SignSession("octocat", fc.Now().Add(time.Hour))}
	for _, tc := range []struct {
		name          string
		origins, refs []string
		allowed       bool
	}{
		{"absent", nil, nil, true},
		{"matching Origin overrides Referer", []string{origin}, []string{sibling}, true},
		{"same-origin Referer", nil, []string{origin + "/admin?tab=all"}, true},
		{"sibling Origin", []string{sibling}, []string{origin}, false},
		{"sibling Referer", nil, []string{sibling + "/admin"}, false},
		{"wrong port Origin", []string{backend}, nil, false},
		{"wrong port Referer", nil, []string{backend + "/admin"}, false},
		{"no public port", []string{"https://" + testBaseDomain}, nil, false},
		{"wrong scheme", []string{"http://" + testBaseDomain + ":9443"}, nil, false},
		{"opaque origin", []string{"null"}, nil, false},
		{"blank Origin", []string{""}, []string{origin}, false},
		{"duplicate Origin", []string{origin, sibling}, nil, false},
		{"duplicate Referer", nil, []string{origin, sibling}, false},
		{"relative Referer", nil, []string{"/admin"}, false},
		{"malformed Referer", nil, []string{"https://%xx/admin"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, route := range []struct {
				method, path  string
				allowedStatus int
			}{
				{http.MethodPost, "/auth/logout", 302},
				{http.MethodPost, "/api/admin/sites/example/permanent", 404},
				{http.MethodDelete, "/api/admin/sites/example", 404},
				{http.MethodPatch, "/api/admin/sites/example", 404},
			} {
				r := httptest.NewRequest(route.method, backend+route.path, nil)
				r.AddCookie(session)
				r.Header.Set("X-Forwarded-Host", "uploaded."+testBaseDomain+":9443")
				for _, v := range tc.origins {
					r.Header.Add("Origin", v)
				}
				for _, v := range tc.refs {
					r.Header.Add("Referer", v)
				}
				want := http.StatusForbidden
				if tc.allowed {
					want = route.allowedStatus
				}
				rec := serveAuth(h, r, want, t)
				if !tc.allowed && findCookie(rec, "__Host-session") != nil {
					t.Fatal("rejected mutation changed the session cookie")
				}
			}
		})
	}
	safe := httptest.NewRequest(http.MethodGet, origin+"/admin", nil)
	safe.Header.Set("Origin", sibling)
	safe.AddCookie(session)
	serveAuth(h, safe, 200, t)
	unauthenticated := httptest.NewRequest(http.MethodDelete, origin+"/api/admin/sites/example", nil)
	unauthenticated.Header.Set("Origin", sibling)
	serveAuth(h, unauthenticated, 401, t)
}

func TestAdminMetricsAndApexOnly(t *testing.T) {
	h, _ := newTestServer(t, nil)
	for _, route := range []struct {
		host, path string
		status     int
	}{
		{testBaseDomain, "/admin", 302},
		{testBaseDomain, "/api/admin/sites", 401},
		{testBaseDomain, "/adminish", 404},
		{testBaseDomain, "/auth/login", 302},
		{"uploaded." + testBaseDomain, "/admin", 404},
		{"uploaded." + testBaseDomain, "/auth/login", 404},
		{"uploaded." + testBaseDomain, "/api/admin/sites", 404},
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+route.host+route.path, nil)
		rec := serveAuth(h, r, route.status, t)
		if route.host != testBaseDomain && len(rec.Result().Cookies()) != 0 {
			t.Fatal("non-apex route issued auth cookies")
		}
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodHead, "/auth/login"},
		{http.MethodHead, "/auth/callback"},
		{http.MethodGet, "/auth/logout"},
	} {
		rec := serveAuth(h, httptest.NewRequest(tc.method, "http://"+testBaseDomain+tc.path, nil), 405, t)
		if len(rec.Result().Cookies()) != 0 {
			t.Fatal("unsupported method touched cookies")
		}
	}
	metric := serveAuth(h, httptest.NewRequest(http.MethodGet, "http://metrics.example/metrics", nil), 200, t).Body.String()
	for _, want := range []string{
		`http_request_duration_seconds_count{class="admin"} 2`,
		`http_request_duration_seconds_count{class="app"} 5`,
		`http_request_duration_seconds_count{class="site"} 3`,
	} {
		if !strings.Contains(metric, want+"\n") {
			t.Errorf("missing histogram count %s", want)
		}
	}
}
