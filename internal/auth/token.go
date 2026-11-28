// Package auth handles GitHub OAuth and signed, store-free admin credentials.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// Signer signs state and session cookies with distinct MAC purposes so a
// credential issued for one purpose cannot authenticate in another.
type Signer struct {
	key []byte
}

func NewSigner(secret string) *Signer { return &Signer{key: []byte(secret)} }

type sessionClaims struct {
	Subject string `json:"sub"`
	Expiry  int64  `json:"exp"`
}

type stateClaims struct {
	Nonce    string `json:"state"`
	Verifier string `json:"verifier"`
	Expiry   int64  `json:"exp"`
}

const (
	sessionPurpose = "nyx-drop/session/v1"
	statePurpose   = "nyx-drop/oauth-state/v1"
)

func (s *Signer) SignSession(subject string, expiry time.Time) string {
	payload, _ := json.Marshal(sessionClaims{subject, expiry.Unix()}) // only a string and an int64
	return s.sign(sessionPurpose, payload)
}

func (s *Signer) VerifySession(token string, now time.Time) (string, bool) {
	var claims sessionClaims
	if !s.verify(sessionPurpose, token, &claims) || claims.Subject == "" || now.Unix() >= claims.Expiry {
		return "", false
	}
	return claims.Subject, true
}

func (s *Signer) SignState(nonce, verifier string, expiry time.Time) string {
	payload, _ := json.Marshal(stateClaims{nonce, verifier, expiry.Unix()})
	return s.sign(statePurpose, payload)
}

// VerifyState also binds the signed cookie to the callback query parameter.
// It returns the PKCE verifier only on successful validation.
func (s *Signer) VerifyState(token, nonce string, now time.Time) (string, bool) {
	var claims stateClaims
	if !s.verify(statePurpose, token, &claims) || claims.Nonce == "" || claims.Verifier == "" ||
		now.Unix() >= claims.Expiry || !hmac.Equal([]byte(claims.Nonce), []byte(nonce)) {
		return "", false
	}
	return claims.Verifier, true
}

var tokenEncoding = base64.RawURLEncoding.Strict()

func (s *Signer) sign(purpose string, payload []byte) string {
	return tokenEncoding.EncodeToString(payload) + "." + tokenEncoding.EncodeToString(s.mac(purpose, payload))
}

func (s *Signer) verify(purpose, token string, claims any) bool {
	// Reject multiline/noncanonical encodings; DecodeString alone ignores CR/LF.
	if len(s.key) == 0 || strings.ContainsAny(token, "\r\n") {
		return false
	}
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	payload, err := tokenEncoding.DecodeString(encoded)
	if err != nil {
		return false
	}
	mac, err := tokenEncoding.DecodeString(signature)
	return err == nil && hmac.Equal(mac, s.mac(purpose, payload)) && json.Unmarshal(payload, claims) == nil
}

func (s *Signer) mac(purpose string, payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(purpose + "\x00"))
	h.Write(payload)
	return h.Sum(nil)
}
