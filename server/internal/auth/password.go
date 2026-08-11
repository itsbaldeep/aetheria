// Package auth implements account credentials: argon2id password hashing,
// email/password validation, and the account store. The portal and the
// authserver both use it; the botclient uses the wire endpoint.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id parameters (OWASP-recommended, OWASP Top 10 mitigation).
// Bumped later with a migration to rehash-on-login if hardware improves.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	argonKeyLen  = 32
	argonSaltLen = 16
)

var (
	ErrWeakPassword = errors.New("password must be at least 8 characters and include a letter and a digit")
	ErrBadEmail     = errors.New("email must look like name@domain.tld")
)

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// HashPassword returns the PHC-format argon2id hash string for the password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	b64salt := base64.RawStdEncoding.EncodeToString(salt)
	b64hash := base64.RawStdEncoding.EncodeToString(hash)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64salt, b64hash), nil
}

// VerifyPassword checks a password against a stored PHC-format hash.
func VerifyPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version, memory, time, threads int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(time), uint32(memory), uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1
}

// ValidateEmail returns ErrBadEmail for malformed addresses. Email is the
// username (brief §3), so it is lowercased and trimmed.
func ValidateEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if !emailRe.MatchString(email) || len(email) > 254 {
		return "", ErrBadEmail
	}
	return email, nil
}

// ValidatePassword enforces a minimum strength policy (brief §10: argon2id
// for storage, reasonable policy on the input side).
func ValidatePassword(pw string) error {
	if len(pw) < 8 || len(pw) > 128 {
		return ErrWeakPassword
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return ErrWeakPassword
	}
	return nil
}
