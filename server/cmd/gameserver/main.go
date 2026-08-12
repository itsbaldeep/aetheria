// gameserver — the authoritative world: 20 Hz tick loop, WebSocket
// protocol endpoint, zones/combat/mobs/etc. See docs/BRIEF.md §3.
// M1 scope: authenticated WS handshake (session token + ban check) then
// ServerHello + Ping/Pong. M2: world presence — EnterWorld, MoveIntent,
// AOI snapshots, position persistence.
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
	"github.com/itsbaldeep/aetheria/server/internal/world"
)

// zoneDefs are the M2 zones (brief §6). Havenport is the safe town (M5),
// Emberfield the open field (600×600). Dungeon instances land in M7.
var zoneDefs = []*world.Zone{
	{ID: "havenport", Name: "Havenport", Safe: true, SizeX: 300, SizeZ: 300},
	{ID: "emberfield", Name: "Emberfield", Safe: false, SizeX: 600, SizeZ: 600},
}

func main() {
	s := &platform.Service{Name: "gameserver"}
	wsAddr := "127.0.0.1:" + platform.Env("AETHERIA_GAME_PORT", "3015")
	ctrlAddr := "127.0.0.1:" + platform.Env("AETHERIA_CONTROL_PORT", "5003")

	ctx := context.Background()
	store, err := auth.NewStore(ctx, pgDSN())
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

	// Content seeds (brief §4): skills, mobs, zones are the single source of
	// truth. The gameserver image ships shared/content.
	content, err := world.LoadContent(platform.Env("AETHERIA_CONTENT_DIR", "shared/content"))
	if err != nil {
		s.Log("fatal", "content load failed", "error", err)
		os.Exit(1)
	}
	s.Log("info", "content loaded", "skills", len(content.Skills), "mobs", len(content.Mobs), "zones", len(content.Zones))

	// World simulation (M2/M3). Position save-back runs on a 30 s write-behind;
	// character level/xp/hp/mp persist on change (SaveChar).
	sim := world.New(world.Options{
		Zones:    zoneDefs,
		Content:  content,
		SavePos:  store.SaveCharacterPosition,
		SaveChar: store.SaveCharacterState,
		MobSpawn: func(sim *world.Sim) {
			world.SpawnMobs(sim, content, world.SpawnBands)
		},
	})
	go sim.Run(ctx)
	// Write-behind flush every 30 s (brief §3: dirty-flag flush).
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			sim.SavePlayerPositions(ctx)
		}
	}()

	hub := wire.NewHub(s, guard, &charLoader{store: store}, sim)
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
	ctrlMux.HandleFunc("/control/ccu", func(w http.ResponseWriter, r *http.Request) {
		platform.JSON(w, http.StatusOK, map[string]any{"ccu": sim.PlayerCount()})
	})
	ctrlMux.HandleFunc("/control/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})

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

// charLoader adapts the auth store into the hub's CharacterLoader interface.
type charLoader struct {
	store *auth.Store
}

func (c *charLoader) LoadCharacter(ctx context.Context, accountID, charID int64) (*wire.CharacterSpawn, error) {
	row, err := c.store.LoadCharacterSpawn(ctx, accountID, charID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, nil
	}
	return &wire.CharacterSpawn{
		ID:     row.ID,
		Name:   row.Name,
		Class:  row.Class,
		ZoneID: row.ZoneID,
		Pos:    row.Pos,
		Level:  row.Level,
		HP:     row.HP,
		MaxHP:  row.MaxHP,
		MP:     row.MP,
		MaxMP:  row.MaxMP,
		XP:     row.XP,
	}, nil
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
