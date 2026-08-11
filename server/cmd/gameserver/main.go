// gameserver — the authoritative world: 20 Hz tick loop, WebSocket
// protocol endpoint, zones/combat/mobs/etc. See docs/BRIEF.md §3.
// M1 scope: authenticated WS handshake (session token + ban check),
// then ServerHello + Ping/Pong. World simulation begins M2.
package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/itsbaldeep/aetheria/server/internal/auth"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
	"github.com/itsbaldeep/aetheria/server/internal/wire"
)

func main() {
	s := &platform.Service{Name: "gameserver"}
	// TLS terminates at Caddy; the gameserver WS + control endpoints only ever
	// see local connections. Never expose game ports directly (§10 guardrails).
	wsAddr := "127.0.0.1:" + platform.Env("AETHERIA_GAME_PORT", "3015")
	ctrlAddr := "127.0.0.1:" + platform.Env("AETHERIA_CONTROL_PORT", "5003")

	// Session guard: validate handshake tokens + re-check bans at connect.
	pgDSN := pgDSN()
	ctx := context.Background()
	store, err := auth.NewStore(ctx, pgDSN)
	if err != nil {
		s.Log("fatal", "db connect failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ttlHours, _ := time.ParseDuration(platform.Env("AETHERIA_SESSION_TTL_HOURS", "24") + "h")
	sessions, err := auth.NewSessionManager(platform.Env("AETHERIA_SESSION_KEY", ""), ttlHours)
	if err != nil {
		s.Log("fatal", "session manager init failed", "error", err)
		os.Exit(1)
	}
	guard := auth.NewGuard(sessions, store)

	hub := wire.NewHub(s, guard)
	go hub.Run()

	// Public WebSocket endpoint (Caddy proxies wss://play.<domain>/ws here).
	http.HandleFunc("/ws", hub.HandleWS)
	http.HandleFunc("/healthz", s.Healthz())
	go func() {
		s.Log("info", "gameserver ws listening", "addr", wsAddr)
		if err := http.ListenAndServe(wsAddr, nil); err != nil {
			s.Log("fatal", "ws server exited", "error", err)
		}
	}()

	// Private control endpoint (localhost-only; adminserver talks to it).
	ctrlMux := http.NewServeMux()
	ctrlMux.HandleFunc("/healthz", s.Healthz())
	ctrlMux.HandleFunc("/control/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	// 20 Hz simulation tick. M0/M1: no world state yet; heartbeat only.
	ctx2, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			select {
			case <-ctx2.Done():
				return
			default:
			}
		}
	}()

	ln, err := net.Listen("tcp", ctrlAddr)
	if err != nil {
		s.Log("fatal", "control listen failed", "error", err)
		return
	}
	s.Log("info", "gameserver control listening", "addr", ctrlAddr)
	if err := http.Serve(ln, ctrlMux); err != nil {
		s.Log("fatal", "control server exited", "error", err)
	}
}

func pgDSN() string {
	if dsn := platform.Env("AETHERIA_PG_DSN", ""); dsn != "" {
		return dsn
	}
	return "postgres://" + platform.Env("AETHERIA_PG_USER", "aetheria") + ":" +
		platform.Env("AETHERIA_PG_PASSWORD", "") + "@" +
		platform.Env("AETHERIA_PG_HOST", "127.0.0.1") + ":" +
		platform.Env("AETHERIA_PG_PORT", "5004") + "/" +
		platform.Env("AETHERIA_PG_DB", "aetheria") + "?sslmode=disable"
}
