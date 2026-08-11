// Guard validates session tokens + ban status for services that don't issue
// tokens themselves (gameserver WS handshake, future admin endpoints).
// It wraps the SessionManager (signature) and Store (ban lookup).
package auth

import (
	"context"
	"errors"
	"time"
)

var ErrAccountBanned = errors.New("account banned")

// Guard is a stateless token+ban checker for the gameserver.
type Guard struct {
	sessions *SessionManager
	store    *Store
}

func NewGuard(sessions *SessionManager, store *Store) *Guard {
	return &Guard{sessions: sessions, store: store}
}

// Validate verifies the token and that the account is not currently banned.
// Returns the account id, or ErrSessionInvalid / ErrAccountBanned.
func (g *Guard) Validate(ctx context.Context, token string) (int64, error) {
	accountID, err := g.sessions.Verify(token)
	if err != nil {
		return 0, ErrSessionInvalid
	}
	bannedUntil, err := g.store.BannedUntil(ctx, accountID)
	if err != nil {
		return 0, err
	}
	if bannedUntil != nil && time.Now().Before(*bannedUntil) {
		return 0, ErrAccountBanned
	}
	return accountID, nil
}
