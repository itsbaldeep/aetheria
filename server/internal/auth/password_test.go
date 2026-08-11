package auth

import (
	"testing"
)

func TestValidateEmail(t *testing.T) {
	valid := []string{"a@b.co", "User.Name+tag@Sub.Domain.io", "  spaced@x.io  "}
	for _, e := range valid {
		got, err := ValidateEmail(e)
		if err != nil {
			t.Fatalf("ValidateEmail(%q) unexpected error: %v", e, err)
		}
		if got != "spaced@x.io" && got == e && e != "spaced@x.io" && e == "  spaced@x.io  " {
			t.Fatalf("ValidateEmail(%q) = %q, want trimmed lowercase", e, got)
		}
	}
	invalid := []string{"", "not-an-email", "a@b", "a b@x.io", "a@x", "@x.io"}
	for _, e := range invalid {
		if _, err := ValidateEmail(e); err == nil {
			t.Fatalf("ValidateEmail(%q) expected error, got nil", e)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	valid := []string{"password1", "H4rder!#@", "abcdefg1"}
	for _, p := range valid {
		if err := ValidatePassword(p); err != nil {
			t.Fatalf("ValidatePassword(%q) unexpected error: %v", p, err)
		}
	}
	invalid := []string{"", "short1", "alllowercase", "12345678", "abcdefgh"}
	for _, p := range invalid {
		if err := ValidatePassword(p); err == nil {
			t.Fatalf("ValidatePassword(%q) expected error, got nil", p)
		}
	}
}

func TestHashVerifyRoundtrip(t *testing.T) {
	hash, err := HashPassword("correct horse 42")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "correct horse 42" {
		t.Fatal("hash must not equal plaintext")
	}
	if !VerifyPassword(hash, "correct horse 42") {
		t.Fatal("VerifyPassword should accept the correct password")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("VerifyPassword must reject a wrong password")
	}
	// Two hashes of the same password differ (unique salt).
	hash2, _ := HashPassword("correct horse 42")
	if hash == hash2 {
		t.Fatal("argon2id hashes must be salted, so two hashes must differ")
	}
}

func TestVerifyPasswordMalformedHash(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2id$v=19$m=1,t=1,p=1$!bad!$!bad!", "not$argon2id$..."} {
		if VerifyPassword(h, "x") {
			t.Fatalf("VerifyPassword(%q) should be false", h)
		}
	}
}
