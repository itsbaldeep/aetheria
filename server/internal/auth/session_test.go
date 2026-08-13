package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSessionIssueVerify(t *testing.T) {
	m, err := NewSessionManager("0123456789abcdef0123456789abcdef", time.Hour)
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	tok, exp, err := m.Issue(42)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tok == "" {
		t.Fatal("Issue returned empty token")
	}
	if exp.Before(time.Now()) {
		t.Fatal("expiresAt must be in the future")
	}
	// The token is a signed JWT (three dot-separated segments).
	if got := len(strings.Split(tok, ".")); got != 3 {
		t.Fatalf("token has %d segments, want 3 (JWT)", got)
	}
	id, err := m.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id != 42 {
		t.Fatalf("Verify id = %d, want 42", id)
	}
}

func TestSessionVerifyRejectsTampering(t *testing.T) {
	m, _ := NewSessionManager("0123456789abcdef0123456789abcdef", time.Hour)
	tok, _, _ := m.Issue(7)

	// Flip a character mid-signature. Flipping the *last* base64url char is
	// unreliable: an HS256 signature is 32 bytes → 43 chars, and the final
	// char's top 2 bits are padding, so a last-char edit often decodes to the
	// same signature bytes and the tampered token still verifies.
	mid := len(tok) - len(tok[len(tok)-22:])
	flip := map[byte]byte{'a': 'b', 'b': 'a', '0': '1', '1': '0'}
	repl, ok := flip[tok[mid]]
	if !ok || repl == tok[mid] {
		if tok[mid] == 'x' {
			repl = 'y'
		} else {
			repl = 'x'
		}
	}
	bad := tok[:mid] + string(repl) + tok[mid+1:]
	if _, err := m.Verify(bad); err == nil {
		t.Fatal("tampered token must not verify")
	}

	// A different key must reject the token.
	m2, _ := NewSessionManager("abcdefabcdefabcdefabcdefabcdefab", time.Hour)
	if _, err := m2.Verify(tok); err == nil {
		t.Fatal("token signed by another key must not verify")
	}
}

func TestSessionManagerRequiresKey(t *testing.T) {
	if _, err := NewSessionManager("short", time.Hour); err == nil {
		t.Fatal("too-short key must error")
	}
}
