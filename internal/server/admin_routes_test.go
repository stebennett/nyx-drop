package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminRouteRequiresSession(t *testing.T) {
	h, _ := newTestServer(t, nil)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/admin", http.StatusFound},
		{"/admin/future", http.StatusFound},
		{"/api/admin", http.StatusUnauthorized},
		{"/api/admin/sites", http.StatusUnauthorized},
		{"/auth/logout", http.StatusUnauthorized},
	} {
		t.Run(tc.path, func(t *testing.T) {
			method := http.MethodGet
			if tc.path == "/auth/logout" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "http://"+testBaseDomain+tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusFound && rec.Header().Get("Location") != "/auth/login" {
				t.Errorf("wrong login redirect: %s", rec.Header().Get("Location"))
			}
			if tc.want == http.StatusUnauthorized && rec.Header().Get("Content-Type") != "application/json" {
				t.Errorf("wrong API content type: %s", rec.Header().Get("Content-Type"))
			}
		})
	}
}
