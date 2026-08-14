package world

import (
	"context"
	"math"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

var testZone = &Zone{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300}

func newTestSim(t *testing.T) (*Sim, []*Player) {
	t.Helper()
	s := New(Options{Zones: []*Zone{testZone}, Tick: 50 * time.Millisecond, Logf: func(f string, a ...any) { t.Logf(f, a...) }})
	return s, nil
}

func spawnTestPlayer(s *Sim, charID int64, pos Vec3, maxSpeed float64) *Player {
	p := &Player{
		Entity:      Entity{Type: TypePlayer, Name: "T", Zone: testZone.ID, Pos: pos, MaxHP: 100, HP: 100, Level: 1},
		CharacterID: charID,
		MaxSpeed:    maxSpeed,
		Outbox:      s.NewPlayerOutbox(),
	}
	p.Ready.Store(true)
	if err := s.Spawn(p); err != nil {
		panic(err)
	}
	return p
}

// collect reads up to n frames from a player's outbox (or until timeout).
// Each outbox frame is an Envelope wrapping a WorldSnapshot; unwrap.
func collect(t *testing.T, p *Player, n int, d time.Duration) []*aet.WorldSnapshot {
	t.Helper()
	var out []*aet.WorldSnapshot
	deadline := time.After(d)
	for len(out) < n {
		select {
		case frame := <-p.Outbox:
			env := &aet.Envelope{}
			if err := proto.Unmarshal(frame, env); err != nil {
				t.Fatalf("unmarshal envelope: %v", err)
			}
			if env.PayloadType != "aetheria.WorldSnapshot" {
				continue
			}
			snap := &aet.WorldSnapshot{}
			if err := proto.Unmarshal(env.Payload, snap); err != nil {
				t.Fatalf("unmarshal snapshot: %v", err)
			}
			out = append(out, snap)
		case <-deadline:
			return out
		}
	}
	return out
}

func TestSpawnAssignsIDs(t *testing.T) {
	s, _ := newTestSim(t)
	a := spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	b := spawnTestPlayer(s, 2, Vec3{10, 0, 10}, 8)
	if a.ID == b.ID || a.ID == 0 {
		t.Fatalf("expected distinct non-zero ids, got %d %d", a.ID, b.ID)
	}
	if s.PlayerCount() != 2 {
		t.Fatalf("player count = %d, want 2", s.PlayerCount())
	}
}

func TestSpawnDuplicateRejected(t *testing.T) {
	s, _ := newTestSim(t)
	spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	if err := s.Spawn(&Player{Entity: Entity{Zone: testZone.ID}, CharacterID: 1, Outbox: s.NewPlayerOutbox()}); err == nil {
		t.Fatal("expected duplicate spawn to error")
	}
}

func TestUnknownZoneRejected(t *testing.T) {
	s, _ := newTestSim(t)
	p := &Player{Entity: Entity{Zone: "nope"}, CharacterID: 9, Outbox: s.NewPlayerOutbox()}
	if err := s.Spawn(p); err == nil {
		t.Fatal("expected unknown zone to error")
	}
}

func TestMoveClampsSpeed(t *testing.T) {
	s, _ := newTestSim(t)
	spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	// Request 800 m/s (hack): must clamp to 8 m/s → after a few ticks the
	// player has moved only the clamped distance (≤ ~1.6 m), never 80 m.
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 800}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	pos := s.PlayerPos(1)
	if pos == nil {
		t.Fatal("player missing")
	}
	dist := pos.Len()
	if dist > 3.0 {
		t.Fatalf("speed not clamped: moved %.3f m (800 m/s would move ~60 m)", dist)
	}
	if dist < 0.2 {
		t.Fatalf("player barely moved: %.3f m (clamped 8 m/s should cover ~1 m)", dist)
	}
}

func TestMoveDirectionStopsOnZero(t *testing.T) {
	s, _ := newTestSim(t)
	spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 8}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	time.Sleep(120 * time.Millisecond)
	posAfterMove := *s.PlayerPos(1)
	// Stop.
	if err := s.SetMove(1, MoveIntent{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	st := s.PlayerState(1)
	if st == nil {
		t.Fatal("player missing")
	}
	if d := st.Pos.Distance(posAfterMove); d > 0.01 {
		t.Fatalf("player kept moving after stop intent: moved %.3f", d)
	}
}

func TestMoveClampedToZoneBounds(t *testing.T) {
	s, _ := newTestSim(t)
	spawnTestPlayer(s, 1, Vec3{295, 0, 0}, 8)
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 8}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)
	time.Sleep(400 * time.Millisecond)
	pos := s.PlayerPos(1)
	if pos == nil {
		t.Fatal("player missing")
	}
	if pos.X > 300.001 {
		t.Fatalf("player escaped zone bounds: x=%.3f", pos.X)
	}
}

func TestAOIEnterAndLeave(t *testing.T) {
	s, _ := newTestSim(t)
	// A at origin; B starts 100 m away in +z (outside A's 90 m AOI), walks
	// through A's range and out the far side. Fast test speed (80 m/s) keeps
	// the travel time short without tripping the speed clamp (MaxSpeed=80).
	a := spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	b := spawnTestPlayer(s, 2, Vec3{0, 0, 100}, 80)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.Run(ctx)

	if err := s.SetMove(2, MoveIntent{Direction: Vec3{0, 0, -1}, Speed: 80}); err != nil {
		t.Fatal(err)
	}

	// A's AOI covers z ∈ [-30, 60]; B starts at z=100, enters at z=60.
	deadline := time.Now().Add(5 * time.Second)
	var seenB *aet.EntityState
	for time.Now().Before(deadline) {
		snaps := collect(t, a, 1, 500*time.Millisecond)
		for _, s := range snaps {
			for _, e := range s.Entities {
				if e.Name == "T" {
					seenB = e
				}
			}
		}
		if seenB != nil {
			break
		}
	}
	if seenB == nil {
		t.Fatal("player A never saw player B entering AOI")
	}
	if seenB.Position.Z > 60 {
		t.Fatalf("B reported at z=%.1f, should have been seen only inside AOI (z<=60)", seenB.Position.Z)
	}

	// B continues past A to z < -30; A must see B despawn.
	deadline = time.Now().Add(5 * time.Second)
	despawned := false
	for time.Now().Before(deadline) {
		snaps := collect(t, a, 4, 2*time.Second)
		for _, s := range snaps {
			for _, id := range s.DespawnIds {
				if id == b.ID {
					despawned = true
				}
			}
		}
		if despawned {
			break
		}
	}
	if !despawned {
		t.Fatal("player B leaving AOI was never despawned for player A")
	}
}

func TestSetMoveValidation(t *testing.T) {
	s, _ := newTestSim(t)
	spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	if err := s.SetMove(1, MoveIntent{Speed: -1}); err == nil {
		t.Fatal("negative speed should be rejected")
	}
	if err := s.SetMove(1, MoveIntent{Speed: 5000}); err == nil {
		t.Fatal("absurd speed should be rejected")
	}
	if err := s.SetMove(99, MoveIntent{}); err == nil {
		t.Fatal("move for non-existent char should error")
	}
}

func TestWalkingOutOfTownTransitionsZone(t *testing.T) {
	s := New(Options{
		Zones: []*Zone{
			{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50},
			{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300},
		},
		Tick: 50 * time.Millisecond,
	})
	p := spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	// Inside the town pocket → safe zone.
	s.mu.Lock()
	zone := p.Zone
	s.mu.Unlock()
	if zone != "havenport" {
		t.Fatalf("spawn zone = %s, want havenport", zone)
	}
	if z := s.zones[p.Zone]; z == nil || !z.Safe {
		t.Fatal("player should be in the safe zone at spawn")
	}
	// Walk far east (beyond the 50 m pocket) for a few seconds.
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 8}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.tickOnce(time.Now())
		s.mu.Lock()
		zone := p.Zone
		pos := p.Pos
		s.mu.Unlock()
		if zone == "emberfield" {
			if pos.X < 50 {
				t.Fatalf("zone flipped at x=%.1f, want beyond town boundary 50", pos.X)
			}
			return
		}
	}
	t.Fatal("player never left the town pocket")
}

// TestWorldClamp walks straight off the world edge; the player must stop at
// the outermost boundary and never escape it.
func TestWorldClamp(t *testing.T) {
	s := New(Options{
		Zones: []*Zone{
			{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50},
			{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300},
		},
		Tick: 50 * time.Millisecond,
	})
	p := spawnTestPlayer(s, 1, Vec3{290, 0, 0}, 8)
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 8}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.tickOnce(time.Now())
		s.mu.Lock()
		pos := p.Pos
		s.mu.Unlock()
		if pos.X > 300 {
			t.Fatalf("player escaped the world at x=%.1f", pos.X)
		}
	}
	// Settled at the edge, not frozen mid-field.
	s.mu.Lock()
	pos := p.Pos
	s.mu.Unlock()
	if math.Abs(pos.X-300) > 0.75 {
		t.Fatalf("player resting at x=%.1f, want clamped to 300", pos.X)
	}
}

func TestTuningSpeedAndRespawn(t *testing.T) {
	s := New(Options{
		Zones:  []*Zone{testZone},
		Tick:   50 * time.Millisecond,
		Logf:   func(f string, a ...any) { t.Logf(f, a...) },
		Tuning: Tuning{SpeedMultiplier: 4, RespawnDelay: 5 * time.Second},
	})
	spawnTestPlayer(s, 1, Vec3{0, 0, 0}, 8)
	// 8 m/s × 4 tuning = 32 m/s. Run 5 ticks (250 ms) → ~8 m east.
	if err := s.SetMove(1, MoveIntent{Direction: Vec3{1, 0, 0}, Speed: 8}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s.tickOnce(time.Now())
	}
	pos := s.PlayerPos(1)
	if pos.X < 6 || pos.X > 10 {
		t.Fatalf("tuned travel x=%.2f, want ~8 m (4x speed)", pos.X)
	}

	// Respawn tuning: kill a mob and check the respawn clock.
	s.mu.Lock()
	def := &MobDef{ID: "forest_boar", Name: "Forest Boar", Level: 1, HP: 50, ZoneID: "emberfield", XPReward: 20}
	s.mobDefs["forest_boar"] = def
	boar := NewMob(def, 500, Vec3{20, 0, 20})
	s.mobs[boar.ID] = boar
	s.grid.Insert(&boar.Entity)
	p := s.players[1]
	now := time.Now()
	s.killMob(boar, p, now)
	s.mu.Unlock()
	delay := boar.respawnAt.Sub(now)
	if delay < 4*time.Second || delay > 6*time.Second {
		t.Fatalf("tuned respawn delay = %s, want ~5 s", delay)
	}
}
