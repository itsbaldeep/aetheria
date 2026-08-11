// Per-IP rate limiting backed by Redis. Keeps registration/login spam off
// the accounts table (brief §10: rate-limit login attempts; registration
// gets the same treatment at the portal endpoint).
package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is a sliding fixed-window counter keyed by an arbitrary string
// (typically "register:<ip>" or "login:<email>|<ip>").
type Limiter struct {
	rdb redis.Cmdable
}

func NewLimiter(rdb redis.Cmdable) *Limiter { return &Limiter{rdb: rdb} }

// Allow reports whether `key` is under `limit` events within `window`.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	cnt, err := l.rdb.Incr(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("auth: rate incr: %w", err)
	}
	if cnt == 1 {
		if err := l.rdb.Expire(ctx, key, window).Err(); err != nil {
			return false, fmt.Errorf("auth: rate expire: %w", err)
		}
	}
	return cnt <= int64(limit), nil
}
