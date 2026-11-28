package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nyx-drop/internal/auth"
)

func TestAdminCallbackRejectsInvalidStateBeforeGitHub(t *testing.T) {
	var calls atomic.Int32
	h, fc, origin := authFixture(t, "https", nil, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(500)
	})
	for _, tc := range []string{"missing", "mismatch", "expired", "tampered", "duplicate cookie", "duplicate state", "missing code", "denied", "bad query"} {
		t.Run(tc, func(t *testing.T) {
			login := serveAuth(h, httptest.NewRequest(http.MethodGet, origin+"/auth/login", nil), 302, t)
			other := serveAuth(h, httptest.NewRequest(http.MethodGet, origin+"/auth/login", nil), 302, t)
			authURL, err := url.Parse(login.Header().Get("Location"))
			if err != nil || authURL.Query().Get("state") == "" || login.Header().Get("Location") == other.Header().Get("Location") {
				t.Fatal("expected unique valid authorize redirects")
			}
			state := assertAuthCookie(t, login, "__Host-oauth-state", true, 600)
			q := url.Values{"state": {authURL.Query().Get("state")}, "code": {testOAuthCode}}
			switch tc {
			case "mismatch":
				q.Set("state", "wrong")
			case "expired":
				fc.Advance(10 * time.Minute)
			case "tampered":
				state.Value = "A" + state.Value[1:] // JSON payload starts with base64url 'e'
			case "duplicate state":
				q.Add("state", q.Get("state"))
			case "missing code":
				q.Del("code")
			case "denied":
				q.Set("error", "access_denied")
			}
			path := origin + "/auth/callback?" + q.Encode()
			if tc == "bad query" {
				path += "&invalid=%"
			}
			r := httptest.NewRequest(http.MethodGet, path, nil)
			if tc != "missing" {
				r.AddCookie(state)
			}
			if tc == "duplicate cookie" {
				r.AddCookie(state)
			}
			res := serveAuth(h, r, 403, t)
			assertAuthCookie(t, res, "__Host-oauth-state", true, -1)
			if findCookie(res, "__Host-session") != nil || calls.Load() != 0 {
				t.Fatal("invalid callback created a session or reached GitHub")
			}
		})
	}
}

func TestAdminCallbackDenialAndProviderFailures(t *testing.T) {
	for _, tc := range []struct {
		name, tokenResponse, userResponse string
		userStatus, want                  int
	}{
		{"wrong user", "", `{"login":"mallory<script>"}`, 200, 403},
		{"token failure", testOAuthSecret, "", 0, 502},
		{"user failure", "", testOAuthToken, 500, 502},
		{"bad identity JSON", "", "not JSON", 200, 502},
		{"no login", "", `{}`, 200, 502},
		{"oversized identity", "", strings.Repeat(" ", 65<<10) + `{"login":"octocat"}`, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs strings.Builder
			h, _, origin := authFixture(t, "https", &logs, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/login/oauth/access_token":
					if tc.tokenResponse != "" {
						http.Error(w, tc.tokenResponse, 500)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(map[string]string{"access_token": testOAuthToken})
				case "/user":
					if tc.userStatus == 0 {
						t.Error("user endpoint called after exchange failed")
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(tc.userStatus)
					io.WriteString(w, tc.userResponse)
				default:
					t.Error("unexpected provider route")
				}
			})
			login := serveAuth(h, httptest.NewRequest(http.MethodGet, origin+"/auth/login", nil), 302, t)
			loc, err := url.Parse(login.Header().Get("Location"))
			if err != nil || loc.Query().Get("state") == "" {
				t.Fatal("missing OAuth state query")
			}
			q := url.Values{"code": {testOAuthCode}, "state": {loc.Query().Get("state")}}
			r := httptest.NewRequest(http.MethodGet, origin+"/auth/callback?"+q.Encode(), nil)
			r.AddCookie(assertAuthCookie(t, login, "__Host-oauth-state", true, 600))
			cb := serveAuth(h, r, tc.want, t)
			assertAuthCookie(t, cb, "__Host-oauth-state", true, -1)
			if findCookie(cb, "__Host-session") != nil || strings.Contains(logs.String(), "admin login") {
				t.Fatal("failed callback became an admin login")
			}
			body := cb.Body.String()
			if tc.want == 403 {
				if cb.Header().Get("Content-Type") != "text/html; charset=utf-8" || !strings.Contains(body, "Not authorized") ||
					!strings.Contains(body, `href="/auth/login"`) || !strings.Contains(body, "mallory&lt;script&gt;") || strings.Contains(body, "<script>") {
					t.Fatal("missing or unsafe unauthorized view")
				}
			}
			for _, sensitive := range []string{testOAuthSecret, testOAuthCode, testOAuthToken, loc.Query().Get("state")} {
				if strings.Contains(logs.String(), sensitive) || strings.Contains(body, sensitive) {
					t.Fatal("sensitive data in logs or callback body")
				}
			}
		})
	}
}

func TestInvalidAdminSessions(t *testing.T) {
	h, fc, origin := authFixture(t, "https", nil, http.NotFound)
	signer := auth.NewSigner(testSessionKey)
	valid := signer.SignSession("octocat", fc.Now().Add(time.Hour))
	for _, tc := range []struct{ name, cookie, value string }{
		{"expired", "__Host-session", signer.SignSession("octocat", fc.Now())},
		{"tampered", "__Host-session", valid + "A"},
		{"wrong admin", "__Host-session", signer.SignSession("old-admin", fc.Now().Add(time.Hour))},
		{"wrong purpose", "__Host-session", signer.SignState("octocat", "tester", fc.Now().Add(time.Hour))},
		{"bare HTTPS cookie", "session", valid},
		{"duplicate", "__Host-session", valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/admin", "/api/admin/sites"} {
				r := httptest.NewRequest(http.MethodGet, origin+path, nil)
				r.AddCookie(&http.Cookie{Name: tc.cookie, Value: tc.value})
				if tc.name == "duplicate" {
					r.AddCookie(&http.Cookie{Name: tc.cookie, Value: tc.value})
				}
				if path == "/admin" {
					if serveAuth(h, r, 302, t).Header().Get("Location") != "/auth/login" {
						t.Fatal("wrong login redirect")
					}
				} else {
					rec := serveAuth(h, r, 401, t)
					var body struct{ Error string }
					if rec.Header().Get("Content-Type") != "application/json" || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error != "unauthorized" {
						t.Fatal("wrong API error format")
					}
				}
			}
		})
	}
}
