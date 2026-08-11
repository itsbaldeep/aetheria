// authserver — accounts, registration, login (argon2id), session tokens,
// character list/create endpoints. See docs/BRIEF.md §3.
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/itsbaldeep/aetheria/server/internal/auth"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

func main() {
	s := &platform.Service{Name: "authserver"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_AUTH_PORT", "3016")

	pgDSN := platform.Env("AETHERIA_PG_DSN", "")
	if pgDSN == "" {
		// Build from parts so the env file stays split and secret-safe.
		pgDSN = "postgres://" + platform.Env("AETHERIA_PG_USER", "aetheria") + ":" +
			platform.Env("AETHERIA_PG_PASSWORD", "") + "@" +
			platform.Env("AETHERIA_PG_HOST", "127.0.0.1") + ":" +
			platform.Env("AETHERIA_PG_PORT", "5004") + "/" +
			platform.Env("AETHERIA_PG_DB", "aetheria") + "?sslmode=disable"
	}

	ctx := context.Background()
	store, err := auth.NewStore(ctx, pgDSN)
	if err != nil {
		s.Log("fatal", "db connect failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	rdb := redis.NewClient(&redis.Options{
		Addr:     platform.Env("AETHERIA_REDIS_HOST", "127.0.0.1") + ":" + platform.Env("AETHERIA_REDIS_PORT", "5005"),
		Password: platform.Env("AETHERIA_REDIS_PASSWORD", ""),
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		s.Log("fatal", "redis connect failed", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()

	ttlHours, _ := time.ParseDuration(platform.Env("AETHERIA_SESSION_TTL_HOURS", "24") + "h")
	session, err := auth.NewSessionManager(platform.Env("AETHERIA_SESSION_KEY", ""), ttlHours)
	if err != nil {
		s.Log("fatal", "session manager init failed", "error", err)
		os.Exit(1)
	}

	api := auth.NewServer(store, auth.NewLimiter(rdb), session)
	api.Logf = s.Log

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	mux.HandleFunc("/auth/register", api.HandleRegister)
	mux.HandleFunc("/auth/login", api.HandleLogin)
	mux.HandleFunc("/auth/characters", api.HandleListCharacters)
	mux.HandleFunc("/auth/characters/create", api.HandleCreateCharacter)

	s.Log("info", "authserver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}
