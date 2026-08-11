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

	// Flip the last character of the signature.
	last := tok[len(tok)-1]
	flip := map[byte]byte{'a': 'b', 'b': 'a', '0': '1', '1': '0'}
	repl, ok := flip[last]
	if !ok {
		repl = 'x'
	}
	bad := tok[:len(tok)-1] + string(repl)
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
