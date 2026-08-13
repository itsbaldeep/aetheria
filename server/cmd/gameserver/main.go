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
// a small pocket inside Emberfield the open field (600×600, everything not in
// town). M3 derives a player's zone from position so walking out of town
// enters the field. Dungeon instances land in M7.
var zoneDefs = []*world.Zone{
	{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50},
	{ID: "emberfield", Name: "Emberfield", Safe: false, MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300},
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
	s.Log("info", "content loaded", "skills", len(content.Skills), "mobs", len(content.Mobs), "zones", len(content.Zones), "items", len(content.Items), "drops", len(content.Drops), "npcs", len(content.NPCs))

	// World simulation (M2/M3). Position save-back runs on a 30 s write-behind;
	// character level/xp/hp/mp persist on change (SaveChar).
	sim := world.New(world.Options{
		Zones:      zoneDefs,
		Content:    content,
		SavePos:    store.SaveCharacterPosition,
		SaveChar:   store.SaveCharacterState,
		SaveLedger: ledgerFlusher(s, store),
		MobSpawn: func(sim *world.Sim) {
			world.SpawnMobs(sim, content, world.SpawnBands)
		},
	})
	go sim.Run(ctx)
	// Write-behind flush every 30 s (brief §3: dirty-flag flush) + gold ledger
	// (M4: sum(ledger) == world gold).
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			sim.SavePlayerPositions(ctx)
			sim.FlushGoldLedger(ctx)
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
	ctrlMux.HandleFunc("/control/stats", func(w http.ResponseWriter, r *http.Request) {
		st := sim.Stats()
		platform.JSON(w, http.StatusOK, map[string]any{
			"ccu":      sim.PlayerCount(),
			"tick_p50": st.TickP50.String(),
			"tick_p99": st.TickP99.String(),
		})
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
		Gold:   row.Gold,
	}, nil
}

// LoadItems satisfies wire.ItemLoader (M4).
func (c *charLoader) LoadItems(ctx context.Context, charID int64) ([]world.Item, error) {
	return c.store.LoadCharacterItems(ctx, charID)
}

// SaveItems satisfies wire.ItemSaver (M4).
func (c *charLoader) SaveItems(ctx context.Context, charID int64, items []world.Item) error {
	return c.store.SaveCharacterItems(ctx, charID, items)
}

// LoadQuests satisfies wire.QuestLoader (M5).
func (c *charLoader) LoadQuests(ctx context.Context, charID int64) ([]world.QuestProgress, error) {
	return c.store.LoadCharacterQuests(ctx, charID)
}

// SaveQuests satisfies wire.QuestSaver (M5).
func (c *charLoader) SaveQuests(ctx context.Context, charID int64, quests map[string]*world.QuestProgress) error {
	return c.store.SaveCharacterQuests(ctx, charID, quests)
}

// ledgerFlusher adapts the auth store's transactional gold ledger into the
// world's SaveLedger hook. Rejected entries (insufficient gold) are logged.
func ledgerFlusher(s *platform.Service, store *auth.Store) func(ctx context.Context, entries []world.LedgerEntry) error {
	return func(ctx context.Context, entries []world.LedgerEntry) error {
		rejected, err := store.ApplyGoldLedger(ctx, entries)
		if err != nil {
			return err
		}
		for _, r := range rejected {
			s.Log("warn", "ledger rejected", "char_id", r.CharID, "delta", r.Amount, "reason", r.Reason)
		}
		return nil
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
