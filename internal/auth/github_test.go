package auth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestGitHubDoesNotFollowCredentialedRedirects(t *testing.T) {
	var followed atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		followed.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	for _, stage := range []string{"token", "user"} {
		t.Run(stage, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/"+stage {
					http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"access_token":"dummy-test-token","token_type":"bearer"}`)
			}))
			defer provider.Close()
			g := NewGitHub("test-client", "test-secret", "https://test.local/auth/callback", GitHubEndpoints{
				TokenURL: provider.URL + "/token", UserURL: provider.URL + "/user",
			})
			if login, err := g.ExchangeLogin(context.Background(), "test-code", "test-verifier"); err == nil || login != "" {
				t.Fatal("credentialed redirect did not fail closed")
			}
			if followed.Load() != 0 {
				t.Fatal("followed credentialed redirect to the destination")
			}
		})
	}
}
