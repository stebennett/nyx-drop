package auth

import (
	"strings"
	"testing"
	"testing/quick"
	"time"
)

func TestSessionRoundTripAndIntegrity(t *testing.T) {
	signer := NewSigner("test session signing key")
	now := time.Unix(1700000000, 0)
	err := quick.Check(func(n uint16, ttl uint16) bool {
		user := "octocat-" + strings.Repeat("x", int(n%40))
		exp := now.Add(time.Duration(ttl) * time.Second)
		token := signer.SignSession(user, exp)
		got, ok := signer.VerifySession(token, exp.Add(-time.Second))
		_, expired := signer.VerifySession(token, exp)
		idx := int(n) % len(token)
		c := byte('A')
		if token[idx] == c {
			c = 'B'
		}
		_, acceptsTampered := signer.VerifySession(token[:idx]+string(c)+token[idx+1:], exp.Add(-time.Second))
		return got == user && ok && !expired && !acceptsTampered
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSignedTokenRejectsInvalidInputAndWrongPurpose(t *testing.T) {
	signer := NewSigner("test signing key")
	other := NewSigner("different test signing key")
	now := time.Unix(1700000000, 0)
	exp := now.Add(time.Minute)
	session := signer.SignSession("octocat", exp)
	state := signer.SignState("nonce", "verifier", exp)
	if got, ok := signer.VerifyState(state, "nonce", now); !ok || got != "verifier" {
		t.Fatal("valid state failed to return the verifier")
	}
	for _, token := range []string{
		state, "", "!.AAAA", "AAAA.!", "\r\n" + session,
		other.SignSession("octocat", exp), NewSigner("").SignSession("octocat", exp),
		signer.SignSession("", exp), signer.SignSession("octocat", now.Add(-time.Second)),
		signer.sign(sessionPurpose, []byte(`not json`)),
	} {
		if _, ok := signer.VerifySession(token, now); ok {
			t.Fatal("invalid session accepted")
		}
	}
	if _, ok := NewSigner("").VerifySession(NewSigner("").SignSession("octocat", exp), now); ok {
		t.Fatal("signer with absent secret accepted a session")
	}
	for _, token := range []string{
		session, signer.SignState("other", "verifier", exp), signer.SignState("", "verifier", exp),
		signer.SignState("nonce", "", exp), signer.SignState("nonce", "verifier", now),
	} {
		if _, ok := signer.VerifyState(token, "nonce", now); ok {
			t.Fatal("invalid state accepted")
		}
	}
}
