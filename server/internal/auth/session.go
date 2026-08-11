// Session tokens (brief §3: short-lived signed tokens, §10: expire ≤ 24 h).
// HS256 JWT issued by authserver at login; gameserver validates the same
// token on the WS handshake (M1-5). The signing key and TTL come from env;
// never store the token — verify it statelessly with the shared key.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrSessionInvalid = errors.New("invalid session token")

// SessionManager signs and verifies account session JWTs.
type SessionManager struct {
	key []byte
	ttl time.Duration
}

// NewSessionManager builds a manager. key must be non-empty (env
// AETHERIA_SESSION_KEY); ttl is the token lifetime (env AETHERIA_SESSION_TTL_HOURS).
func NewSessionManager(key string, ttl time.Duration) (*SessionManager, error) {
	if len(key) < 16 {
		return nil, errors.New("auth: session key too short (min 16 bytes)")
	}
	return &SessionManager{key: []byte(key), ttl: ttl}, nil
}

type sessionClaims struct {
	AccountID int64 `json:"aid"`
	jwt.RegisteredClaims
}

// Issue creates a signed token valid for the manager's TTL.
func (m *SessionManager) Issue(accountID int64) (token string, expiresAt time.Time, err error) {
	now := time.Now().UTC()
	expiresAt = now.Add(m.ttl)
	claims := sessionClaims{
		AccountID: accountID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "aetheria-auth",
			Subject:   strconv.FormatInt(accountID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.key)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign token: %w", err)
	}
	return t, expiresAt, nil
}

// Verify parses and checks a token. Returns the account id or ErrSessionInvalid.
func (m *SessionManager) Verify(token string) (int64, error) {
	claims := &sessionClaims{}
	t, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrSessionInvalid
		}
		return m.key, nil
	})
	if err != nil || !t.Valid {
		return 0, ErrSessionInvalid
	}
	return claims.AccountID, nil
}
