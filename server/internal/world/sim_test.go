package world

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

var testZone = &Zone{ID: "emberfield", Name: "Emberfield", SizeX: 600, SizeZ: 600}

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
