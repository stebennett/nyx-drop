package server

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// guard authenticates before applying the CSRF rule. A valid signature
// alone isn't enough; the session must still name the configured admin.
func (a *adminAuth) guard(page bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authResponseHeaders(w)
		subject, valid := a.signer.VerifySession(a.readCookie(r, sessionCookie), a.clock.Now())
		if !valid || !a.isAdmin(subject) {
			if page {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
			} else {
				writeAdminError(w, http.StatusUnauthorized, "unauthorized")
			}
			return
		}
		if !adminOriginAllowed(r, a.origin) {
			writeAdminError(w, http.StatusForbidden, "origin does not match")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminOriginAllowed(r *http.Request, expected string) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	// Same-site sibling subdomains may trigger requests carrying Lax
	// cookies. Compare the complete public origin, not the request Host or
	// a forwarded header. A supplied Origin takes precedence over Referer.
	if origins := r.Header.Values("Origin"); len(origins) != 0 {
		return len(origins) == 1 && origins[0] == expected
	}
	refs := r.Header.Values("Referer")
	if len(refs) == 0 {
		return true // the spec checks these headers when present
	}
	if len(refs) != 1 {
		return false
	}
	ref, err := url.Parse(refs[0])
	return err == nil && ref.User == nil && ref.Fragment == "" && ref.Opaque == "" &&
		ref.Scheme+"://"+ref.Host == expected
}

func authResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeAdminError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
