// Scenario: M2 acceptance — "two clients see each other's movement with
// correct AOI". Two bots enter the world near each other, walk apart, and
// assert (a) each sees the other spawn, (b) each sees the other's position
// advance, (c) when far apart each sees the other despawn.
package scenarios

import (
	"context"
	"fmt"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// PresenceResult summarizes an M2 presence run.
type PresenceResult struct {
	ASawBSpawn     bool
	BSawASpawn     bool
	ASawBMove      bool
	BSawAMove      bool
	ASawBDespawn   bool
	BSawADespawn   bool
	ASnapshotCount int
	BSnapshotCount int
}

// Presence runs the two-client mutual-visibility scenario. chars is
// [charIDA, charIDB] owned by their respective tokens (tokens[0] owns
// chars[0], etc.).
func Presence(wsURL string, tokens []string, chars []int64, names []string, timeout time.Duration) (*PresenceResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	a, err := ConnectWorld(ctx, wsURL, tokens[0], chars[0])
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect A: %w", err)
	}
	defer a.Close()
	b, err := ConnectWorld(ctx, wsURL, tokens[1], chars[1])
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect B: %w", err)
	}
	defer b.Close()

	res := &PresenceResult{}

	// Phase 1: both walk toward the same center (they spawn at char pos which
	// may be the same place). A goes +x, B goes -x so they cross and part.
	_ = a.Move(ctx, 1, 0, a.MaxSpeed)
	_ = b.Move(ctx, -1, 0, b.MaxSpeed)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !a.ReadSnapshot(ctx) {
			return nil, fmt.Errorf("scenarios: A disconnected mid-run")
		}
		if !b.ReadSnapshot(ctx) {
			return nil, fmt.Errorf("scenarios: B disconnected mid-run")
		}
		res.ASnapshotCount++
		res.BSnapshotCount++
		if !res.ASawBSpawn {
			if _, ok := a.Seen[b.EntityID]; ok {
				res.ASawBSpawn = true
			}
		}
		if !res.BSawASpawn {
			if _, ok := b.Seen[a.EntityID]; ok {
				res.BSawASpawn = true
			}
		}
		if res.ASawBSpawn && res.BSawASpawn {
			break
		}
	}
	if !res.ASawBSpawn || !res.BSawASpawn {
		return res, fmt.Errorf("scenarios: mutual spawn not observed (A=%v B=%v)", res.ASawBSpawn, res.BSawASpawn)
	}

	// Phase 2: while moving, each must see the other's position change.
	lastA := a.Seen[a.EntityID]
	lastB := b.Seen[b.EntityID]
	deadline = time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !a.ReadSnapshot(ctx) || !b.ReadSnapshot(ctx) {
			break
		}
		// Track the OTHER bot's reported state.
		if be := a.FindEntity(b.EntityID); be != nil && lastB != nil {
			if movedBy(be, lastB) {
				res.ASawBMove = true
			}
			lastB = be
		}
		if ae := b.FindEntity(a.EntityID); ae != nil && lastA != nil {
			if movedBy(ae, lastA) {
				res.BSawAMove = true
			}
			lastA = ae
		}
		if res.ASawBMove && res.BSawAMove {
			break
		}
	}

	// Phase 3: both continue moving away until each despawns the other.
	deadline = time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if !a.ReadSnapshot(ctx) || !b.ReadSnapshot(ctx) {
			break
		}
		if !res.ASawBDespawn {
			for _, id := range a.Despawned {
				if id == b.EntityID {
					res.ASawBDespawn = true
				}
			}
		}
		if !res.BSawADespawn {
			for _, id := range b.Despawned {
				if id == a.EntityID {
					res.BSawADespawn = true
				}
			}
		}
		if res.ASawBDespawn && res.BSawADespawn {
			break
		}
	}

	return res, nil
}

func movedBy(a, b *aet.EntityState) bool {
	return a.Position.X != b.Position.X || a.Position.Z != b.Position.Z
}
