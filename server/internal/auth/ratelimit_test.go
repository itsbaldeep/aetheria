package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Uses real Redis when AETHERIA_REDIS_* env is present; skips otherwise so
// `make test` stays hermetic. The bot register scenario also exercises it.
func TestLimiter(t *testing.T) {
	host := getEnv("AETHERIA_REDIS_HOST", "")
	if host == "" {
		t.Skip("Redis not configured; covered by make bottest")
	}
	port := getEnv("AETHERIA_REDIS_PORT", "5005")
	rdb := redis.NewClient(&redis.Options{
		Addr:     host + ":" + port,
		Password: getEnv("AETHERIA_REDIS_PASSWORD", ""),
	})
	ctx := context.Background()
	key := "test:lim:" + time.Now().Format("20060102T150405.000")
	defer rdb.Del(ctx, key)

	l := NewLimiter(rdb)
	for i := 1; i <= 3; i++ {
		ok, err := l.Allow(ctx, key, 3, time.Minute)
		if err != nil {
			t.Fatalf("Allow #%d: %v", i, err)
		}
		if !ok {
			t.Fatalf("Allow #%d = false, want true (under limit)", i)
		}
	}
	ok, err := l.Allow(ctx, key, 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow #4: %v", err)
	}
	if ok {
		t.Fatal("Allow #4 = true, want false (limit exceeded)")
	}
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
