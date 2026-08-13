// Scenario: M4 acceptance — "a bot loots a boar kill, picks up the ground
// drop, and sells it to a vendor" (brief §212). The bot walks out of the safe
// town pocket, auto-attacks boars until one drops loot, walks onto the ground
// drop, picks it up (asserting a LootEvent with ok=true and the item's new
// instance id), then sells it and asserts the gold balance increased.
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

// TraderResult summarizes an M4 economy run.
type TraderResult struct {
	KilledBoar    bool
	PickedUp      bool
	Sold          bool
	GoldBefore    int64
	GoldAfter     int64
	SnapshotCount int
}

// Trader runs the loot → pickup → sell scenario.
func Trader(wsURL, token string, charID int64, timeout time.Duration, dbg io.Writer) (*TraderResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	b, err := connectWorldRetry(ctx, wsURL, token, charID)
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect: %w", err)
	}
	defer b.Close()

	b.StartHeartbeat(ctx, 4*time.Second)

	res := &TraderResult{}
	var pickedItemID uint64

	// All 10 band-1 anchors to orbit while hunting. Only one forest_boar
	// exists (one instance per def), deterministically placed on a band-1
	// anchor; scanning every anchor keeps it in AOI wherever it landed.
	orbit := [][2]float64{
		{70, -60}, {120, -40}, {60, 40}, {110, 90}, {40, -110},
		{140, 120}, {90, -150}, {160, -80}, {50, 160}, {180, 30},
	}
	orbitIdx := 0
	orbitTarget := orbit[0]
	advanceOrbit := func() {
		if math.Hypot(orbitTarget[0]-b.PosX, orbitTarget[1]-b.PosZ) < 2 {
			orbitIdx = (orbitIdx + 1) % len(orbit)
			orbitTarget = orbit[orbitIdx]
		}
	}

	phase := "leaving"
	start := time.Now()
	loopEnd := start.Add(timeout)
	for time.Now().Before(loopEnd) {
		if res.Sold {
			break
		}
		if ok, err := b.ReadSnapshotErr(ctx); !ok {
			if res.PickedUp {
				break
			}
			if ctx.Err() != nil {
				return nil, fmt.Errorf("scenarios: cycle budget exceeded in phase %s: %w", phase, ctx.Err())
			}
			fmt.Fprintf(dbg, "DISC char=%d phase=%s el=%ds snaps=%d err=%v\n",
				charID, phase, int(time.Since(start).Seconds()), res.SnapshotCount, err)
			return nil, fmt.Errorf("scenarios: disconnected in phase %s: %v", phase, err)
		}
		res.SnapshotCount++
		fmt.Fprintf(dbg, "t=%d phase=%s pos=(%.0f,%.0f) selfhp=%d frames=%d\n",
			int(time.Since(start).Seconds()), phase, b.PosX, b.PosZ, b.LastSelfHP, res.SnapshotCount)

		// Locate nearest living Forest Boar (the only band-1 mob with a drop
		// table). We deliberately avoid Ashmaw (band 3) and other band-1 mobs
		// (no loot): this scenario must kill boars and survive.
		var target *aet.EntityState
		var targetDist float64
		if t := b.FindHostile(); t != nil && t.Hp > 0 && t.MaxHp <= 100 && strings.Contains(t.Name, "Boar") {
			target = t
			dx := float64(t.Position.X) - b.PosX
			dz := float64(t.Position.Z) - b.PosZ
			targetDist = math.Hypot(dx, dz)
		}

		// Respawn if we somehow died (e.g. a stray band-3 mob walked over).
		for _, ev := range b.DrainCombat() {
			if ev.EventType == "xp" {
				res.KilledBoar = true
			}
			if ev.EventType == "death" {
				_ = b.Respawn(ctx)
			}
		}
		for _, le := range b.DrainLoot() {
			if le.Ok && le.ItemDefId != "" && !res.PickedUp {
				res.PickedUp = true
				pickedItemID = le.ItemId
				res.GoldBefore = le.Balance
			}
			if le.Ok && le.Gold > 0 && res.PickedUp {
				res.Sold = true
				res.GoldAfter = le.Balance
			}
		}

		// Steer onto a ground drop and pick it up once in range.
		if !res.PickedUp {
			if drop := b.FindDrop(); drop != nil {
				dx := float64(drop.Position.X) - b.PosX
				dz := float64(drop.Position.Z) - b.PosZ
				if math.Hypot(dx, dz) <= 2.8 {
					_ = b.PickupItem(ctx, drop.EntityId)
				} else if err := b.steer(ctx, float64(drop.Position.X), float64(drop.Position.Z)); err != nil {
					fmt.Fprintf(dbg, "ERR steer(drop): %v\n", err)
				}
				continue
			}
		}

		// Sell the picked-up item (boar hides are misc, always sellable).
		if res.PickedUp && !res.Sold && pickedItemID != 0 {
			_ = b.SellItem(ctx, pickedItemID, 1)
		}

		// Movement: orbit band-1 anchors until a boar is in AOI, then engage.
		// The bot must keep moving: the sim emits snapshots only on change, so
		// parking on one anchor would freeze the read loop and stop the scan.
		switch phase {
		case "leaving":
			if target != nil {
				phase = "hunting"
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					fmt.Fprintf(dbg, "ERR steer(leaving-sea): %v\n", err)
				}
			} else {
				advanceOrbit()
				if err := b.steer(ctx, orbitTarget[0], orbitTarget[1]); err != nil {
					fmt.Fprintf(dbg, "ERR steer(leaving): %v\n", err)
				}
			}
		case "hunting":
			if target == nil {
				advanceOrbit()
				if err := b.steer(ctx, orbitTarget[0], orbitTarget[1]); err != nil {
					fmt.Fprintf(dbg, "ERR steer(hunting-nil): %v\n", err)
				}
				break
			}
			if targetDist > 5 {
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					fmt.Fprintf(dbg, "ERR steer: %v\n", err)
				}
			} else if target.EntityId != b.autoTarget {
				b.autoTarget = target.EntityId
				_ = b.Stop(ctx)
				_ = b.AutoAttack(ctx, target.EntityId, true)
			}
		}
	}

	if res.PickedUp {
		fmt.Printf("trader OK: killed boar=%v picked=%v sold=%v gold %d→%d\n",
			res.KilledBoar, res.PickedUp, res.Sold, res.GoldBefore, res.GoldAfter)
	}
	return res, nil
}
