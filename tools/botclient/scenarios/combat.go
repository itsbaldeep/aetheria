// Scenario: M3 acceptance — "bot kills a boar and gains XP; dies to Ashmaw
// and respawns at shrine" (brief §11 M3). The bot walks out of the safe town
// pocket, auto-attacks a boar until it dies (asserting XP), then pulls Ashmaw,
// dies, respawns, and lands back at full HP.
package scenarios

import (
	"context"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// CombatResult summarizes an M3 combat run.
type CombatResult struct {
	KilledBoar     bool
	XPgained       int64
	Died           bool
	Respawned      bool
	HPAfterRespawn int64
	SnapshotCount  int
	// NegativeHPSeen is set if any self snapshot reported HP < 0 (soak assertion).
	NegativeHPSeen bool
}

// ashmawSpawn is the deterministic band-3 anchor the spawner lands near when
// defs are iterated in sorted-id order (see world.SpawnMobs). The bot steers
// to this point to pull Ashmaw.
const ashmawSpawnX, ashmawSpawnZ = 261, -255

// Combat runs the boar-kill → Ashmaw-death → respawn scenario.
func Combat(wsURL, token string, charID int64, timeout time.Duration, dbg io.Writer) (*CombatResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	b, err := connectWorldRetry(ctx, wsURL, token, charID)
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect: %w", err)
	}
	defer b.Close()

	// Idle phases (standing in range, waiting for aggro) send no snapshots, so
	// the read loop can block for minutes; the server's inbound read loop would
	// idle the socket in 10 s. A concurrent heartbeat keeps it fed.
	b.StartHeartbeat(ctx, 4*time.Second)

	res := &CombatResult{}

	phase := "leaving"
	start := time.Now()
	loopEnd := start.Add(timeout)
	lastKeepAlive := time.Now()
	for time.Now().Before(loopEnd) {
		if ok, err := b.ReadSnapshotErr(ctx); !ok {
			if res.Died && res.Respawned {
				break
			}
			return nil, fmt.Errorf("scenarios: disconnected in phase %s: %v", phase, err)
		}
		res.SnapshotCount++
		if b.LastSelfHP < 0 {
			res.NegativeHPSeen = true
		}

		{
			ents := ""
			for _, e := range b.Seen {
				if e.EntityType == "mob" {
					ents += fmt.Sprintf(" [%d:%s@%.0f,%.0f hp%d]", e.EntityId, e.Name, e.Position.X, e.Position.Z, e.Hp)
				}
			}
			fmt.Fprintf(dbg, "t=%d phase=%s pos=(%.0f,%.0f) selfhp=%d frames=%d mobs:%s\n", int(time.Since(start).Seconds()), phase, b.PosX, b.PosZ, b.LastSelfHP, res.SnapshotCount, ents)
		}

		// Keep the server's inbound read loop alive: send a Ping at least
		// every 5 s even when the bot has nothing to say (server idles the
		// connection after 10 s of silence).
		if time.Since(lastKeepAlive) > 5*time.Second {
			_ = b.KeepAlive(ctx)
			lastKeepAlive = time.Now()
		}

		// Locate the nearest living hostile (any zone/band).
		var target *aet.EntityState
		var targetDist float64
		if t := b.FindHostile(); t != nil {
			if t.Hp <= 0 {
				b.AutoAttack(ctx, 0, false)
			} else {
				target = t
				dx := float64(t.Position.X) - b.PosX
				dz := float64(t.Position.Z) - b.PosZ
				targetDist = math.Hypot(dx, dz)
			}
		}

		// Process any combat events for us.
		for _, ev := range b.DrainCombat() {
			switch ev.EventType {
			case "xp":
				if res.XPgained == 0 {
					res.XPgained = ev.Amount
				}
				res.KilledBoar = true
			case "death":
				res.Died = true
				_ = b.Respawn(ctx)
			case "respawn":
				res.Respawned = true
				res.HPAfterRespawn = b.LastSelfHP
			}
		}

		// Once the boar is dead (and we're alive), break off and head deep
		// into the field to Ashmaw's band-3 anchor.
		if res.KilledBoar && !res.Died && phase == "hunting" {
			phase = "seek-ashmaw"
			_ = b.AutoAttack(ctx, 0, false)
		}

		switch phase {
		case "leaving":
			// Walk +x,+z out of the town pocket toward band 1. If a hostile is
			// already visible from town (AOI peeks past the safe pocket), lock
			// on and steer this same frame — the sim emits the next snapshot
			// only when something changes, so we must start moving now.
			if target != nil {
				phase = "hunting"
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					fmt.Fprintf(dbg, "ERR steer(leaving-sea): %v\n", err)
				}
			} else if err := b.Move(ctx, 1, 1, b.MaxSpeed); err != nil {
				fmt.Fprintf(dbg, "ERR move(leaving): %v\n", err)
			}
		case "hunting":
			if target == nil {
				// No hostile in AOI: orbit a band-1 spawn anchor (near town) so
				// the bot re-scans mobs instead of drifting to a world corner.
				if err := b.steer(ctx, 70, -60); err != nil {
					fmt.Fprintf(dbg, "ERR steer(hunting-nil): %v\n", err)
				}
				break
			}
			if targetDist > 5 {
				// Still too far: walk the rest of the way in.
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					fmt.Fprintf(dbg, "ERR steer: %v\n", err)
				}
			} else if target.EntityId != b.autoTarget {
				// In range: stop and auto-attack.
				b.autoTarget = target.EntityId
				_ = b.Stop(ctx)
				_ = b.AutoAttack(ctx, target.EntityId, true)
			}
		case "seek-ashmaw":
			// Steer to Ashmaw's anchor; once inside its 12 m aggro radius, stand
			// and die (the pull becomes the point).
			if target != nil && targetDist < 8 {
				if b.autoTarget != target.EntityId {
					b.autoTarget = target.EntityId
					_ = b.Stop(ctx)
					_ = b.AutoAttack(ctx, target.EntityId, true)
				}
			} else if err := b.steer(ctx, ashmawSpawnX, ashmawSpawnZ); err != nil {
				fmt.Fprintf(dbg, "ERR steer-ashmaw: %v\n", err)
			}
		}

		if res.Died && !res.Respawned {
			// Respawn ack should arrive; keep reading frames until the server
			// confirms (a respawned self snapshot appears with HP > 0).
			_ = b.Respawn(ctx)
		}
		if res.Died && res.Respawned && b.LastSelfHP > 0 {
			break
		}
	}

	if res.KilledBoar {
		fmt.Printf("combat OK: killed boar, gained %d XP\n", res.XPgained)
	}
	return res, nil
}

// connectWorldRetry connects, retrying on "already_in_world". When a previous
// cycle's socket is still being reaped server-side, an immediate re-enter is
// rejected; a brief retry rides out the teardown race.
func connectWorldRetry(ctx context.Context, wsURL, token string, charID int64) (*WorldBot, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(500 * time.Millisecond):
			}
		}
		b, err := ConnectWorld(ctx, wsURL, token, charID)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if strings.Contains(err.Error(), "already_in_world") {
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

// steer sends a MoveIntent aimed at a world position (direction unit vector).
func (b *WorldBot) steer(ctx context.Context, tx, tz float64) error {
	dx, dz := tx-b.PosX, tz-b.PosZ
	d := math.Hypot(dx, dz)
	if d < 0.5 {
		return b.Stop(ctx)
	}
	if d > 60 {
		// Far away: clamp the heading (we don't need sub-metre precision).
		dx, dz = dx*60/d, dz*60/d
	}
	return b.Move(ctx, dx, dz, b.MaxSpeed)
}
