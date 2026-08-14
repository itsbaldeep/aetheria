// Scenario: M5 acceptance — a bot plays the full Havenport quest chain (brief
// §141): talks to the quest giver, accepts quests, kills quest mobs, collects
// quest drops, buys gear from the vendor, turns in each quest, and levels
// from 1 to ~9. It drives the real QuestAccept/NpcInteract/QuestTurnIn wire
// messages and asserts every quest in the 15-quest chain reaches complete.
package scenarios

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// QuesterResult summarizes an M5 quest-chain run.
type QuesterResult struct {
	Completed       int32 // quests turned in
	FinalLevel      int32 // character level at the end
	TotalTurnIns    int32
	TotalKills      int32
	TotalPickups    int32
	SnapshotCount   int
	QuestsCompleted map[string]bool
	KillSpots       map[string][]dropSpot // mob def → recent death positions
}

// dropSpot is where a mob died — ground drops spawn there (M5 collect steps).
type dropSpot struct{ X, Z float64 }

// questDef is the static drive-table for the Havenport chain (mirrors the
// shared/content/quests seeds). Band anchors let the bot hunt by mob def.
type questStep struct {
	ID           string
	Giver        string
	Turnin       string
	MobDef       string // "" for pure talk quests
	Band         int    // mob spawn band (for hunting)
	Collect      string // item def to collect via pickup/buy ("" for none)
	CollectIsBuy bool   // collect satisfied by buying from a vendor
	SourceMob    string // mob that drops Collect (for ground-drop collect steps)
	Talk         string // npc def to talk to ("" for none; interact always happens)
}

var questChain = []questStep{
	{ID: "q_welcome", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", Talk: "aldric_questgiver"},
	{ID: "q_boar_pests", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "forest_boar", Band: 1},
	{ID: "q_hide_collector", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", Collect: "boar_hide", SourceMob: "forest_boar"},
	{ID: "q_wolf_threat", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ember_wolf", Band: 1},
	{ID: "q_rat_plague", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "cinder_rat", Band: 1},
	{ID: "q_thorn_clearing", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "thorn_viper", Band: 1},
	{ID: "q_gear_up", Giver: "aldric_questgiver", Turnin: "vendor_maren", Collect: "iron_sword", CollectIsBuy: true, Talk: "vendor_maren"},
	{ID: "q_hound_hunt", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ember_hound", Band: 2},
	{ID: "q_brute_force", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ash_brute", Band: 2},
	{ID: "q_wraith_warden", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "cinder_wraith", Band: 2},
	{ID: "q_magma_stopper", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "magma_boar", Band: 2},
	{ID: "q_lava_crawler_clear", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "lava_crawler", Band: 3},
	{ID: "q_ashmaw_fang", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ashmaw", Band: 3, Collect: "ashmaw_fang", SourceMob: "ashmaw"},
	{ID: "q_field_report", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", Talk: "aldric_questgiver"},
	{ID: "q_hero_welcome", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", Talk: "aldric_questgiver"},
}

// npcPos maps npc def id → world position (Havenport).
var npcPos = map[string][2]float64{
	"aldric_questgiver": {10, -8},
	"vendor_maren":      {-12, 8},
}

// bandAnchors mirrors world.SpawnBands for hunting.
var bandAnchors = map[int][][2]float64{
	1: {{70, -60}, {120, -40}, {60, 40}, {110, 90}, {40, -110}, {140, 120}, {90, -150}, {160, -80}, {50, 160}, {180, 30}},
	2: {{-160, 140}, {-110, 180}, {-200, 60}, {-140, -100}, {-60, 200}, {-220, -60}, {-90, -180}, {-170, -160}, {-240, 120}, {-30, -220}},
	3: {{-280, -200}, {260, -260}, {200, 260}, {-250, 250}, {280, 140}, {240, -140}, {-180, -280}, {300, -40}, {-300, 40}, {140, 300}},
}

// Quester plays the full 15-quest chain against a live/staging gameserver.
func Quester(wsURL, token string, charID int64, timeout time.Duration, dbg io.Writer) (*QuesterResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	b, err := connectWorldRetry(ctx, wsURL, token, charID)
	if err != nil {
		return nil, fmt.Errorf("scenarios: connect: %w", err)
	}
	defer b.Close()
	b.StartHeartbeat(ctx, 4*time.Second)

	res := &QuesterResult{QuestsCompleted: map[string]bool{}, KillSpots: map[string][]dropSpot{}}
	start := time.Now()
	loopEnd := start.Add(timeout)

	for _, step := range questChain {
		if time.Now().After(loopEnd) {
			return nil, fmt.Errorf("scenarios: quester budget exceeded at quest %s", step.ID)
		}
		if err := runQuestStep(ctx, b, step, res, dbg, loopEnd); err != nil {
			return nil, err
		}
	}

	res.SnapshotCount = b.SnapshotCount

	// After the chain, confirm the level.
	status := b.LastQuestStatus
	if status != nil {
		for _, qs := range status.Quests {
			if qs.State == "complete" {
				res.QuestsCompleted[qs.QuestId] = true
			}
		}
	}
	fmt.Printf("quester OK: %d/%d quests complete, level %d\n",
		len(res.QuestsCompleted), len(questChain), res.FinalLevel)
	return res, nil
}

// runQuestStep drives one quest from accept → objectives → turn-in.
func runQuestStep(ctx context.Context, b *WorldBot, step questStep, res *QuesterResult, dbg io.Writer, loopEnd time.Time) error {
	// 1. Walk to the giver and accept the quest.
	giver := npcPos[step.Giver]
	if err := walkTo(ctx, b, giver[0], giver[1], 2.5); err != nil {
		return fmt.Errorf("scenarios: walk to giver %s: %w", step.Giver, err)
	}
	// Interact twice: once to load the dialog (the server pushes an
	// NpcDialogEvent), once to accept.
	if err := b.NpcInteract(ctx, step.Giver); err != nil {
		return fmt.Errorf("scenarios: interact %s: %w", step.Giver, err)
	}
	if waitDialog(ctx, b, step.Giver) == nil {
		return fmt.Errorf("scenarios: dialog %s: no dialog received", step.Giver)
	}
	// Accept the quest.
	if err := b.QuestAccept(ctx, step.ID); err != nil {
		return fmt.Errorf("scenarios: accept %s: %w", step.ID, err)
	}
	// Drain the QuestEvent ack for the accept.
	waitQuestEvent(ctx, b, step.ID, "active")

	// 2. Advance every objective.
	if step.MobDef != "" {
		kills, err := huntMob(ctx, b, step.MobDef, int32(step.Band), 3, res, dbg, loopEnd)
		if err != nil {
			return fmt.Errorf("scenarios: hunt %s: %w", step.MobDef, err)
		}
		res.TotalKills += kills
	}
	if step.Collect != "" {
		if step.CollectIsBuy {
			// Buy the item from the turn-in vendor (Maren).
			pos := npcPos[step.Turnin]
			if err := walkTo(ctx, b, pos[0], pos[1], 2.5); err != nil {
				return fmt.Errorf("scenarios: walk to vendor: %w", err)
			}
			if err := b.BuyItem(ctx, step.Turnin, step.Collect, 1); err != nil {
				return fmt.Errorf("scenarios: buy %s: %w", step.Collect, err)
			}
			res.TotalPickups++
		} else {
			n, err := collectDrops(ctx, b, step.Collect, step.SourceMob, 3, res, dbg, loopEnd)
			if err != nil {
				return fmt.Errorf("scenarios: collect %s: %w", step.Collect, err)
			}
			res.TotalPickups += n
		}
	}
	if step.Talk != "" {
		pos := npcPos[step.Talk]
		if err := walkTo(ctx, b, pos[0], pos[1], 2.5); err != nil {
			return fmt.Errorf("scenarios: walk to talk %s: %w", step.Talk, err)
		}
		if err := b.NpcInteract(ctx, step.Talk); err != nil {
			return fmt.Errorf("scenarios: talk %s: %w", step.Talk, err)
		}
		if waitDialog(ctx, b, step.Talk) == nil {
			return fmt.Errorf("scenarios: talk dialog %s: no dialog received", step.Talk)
		}
	}

	// 3. Walk to the turn-in NPC and turn in.
	ti := npcPos[step.Turnin]
	if err := walkTo(ctx, b, ti[0], ti[1], 2.5); err != nil {
		return fmt.Errorf("scenarios: walk to turnin: %w", err)
	}
	if err := b.NpcInteract(ctx, step.Turnin); err != nil {
		return fmt.Errorf("scenarios: interact turnin: %w", err)
	}
	waitDialog(ctx, b, step.Turnin)
	if err := b.QuestTurnIn(ctx, step.ID); err != nil {
		return fmt.Errorf("scenarios: turnin %s: %w", step.ID, err)
	}
	if ev := waitQuestEvent(ctx, b, step.ID, "complete"); ev == nil || ev.State != "complete" {
		if msg := questError(b, step.ID); msg != "" {
			return fmt.Errorf("scenarios: quest %s rejected at turn-in: %s", step.ID, msg)
		}
		return fmt.Errorf("scenarios: quest %s not complete after turn-in", step.ID)
	}
	res.Completed++
	res.QuestsCompleted[step.ID] = true
	res.TotalTurnIns++
	// Track the character level from the last self snapshot.
	if lvl := b.SelfLevel(); lvl > res.FinalLevel {
		res.FinalLevel = lvl
	}
	fmt.Fprintf(dbg, "quester: +%s (level %d)\n", step.ID, res.FinalLevel)
	return nil
}

// walkTo steers the bot to a world position and reads snapshots until within
// `radius` metres (or the per-step budget expires).
func walkTo(ctx context.Context, b *WorldBot, tx, tz, radius float64) error {
	deadline := time.Now().Add(90 * time.Second)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("walk to (%.0f,%.0f) timed out at (%.0f,%.0f)", tx, tz, b.PosX, b.PosZ)
		}
		if math.Hypot(tx-b.PosX, tz-b.PosZ) <= radius {
			_ = b.Stop(ctx)
			return nil
		}
		if err := b.steer(ctx, tx, tz); err != nil {
			return err
		}
		if !b.ReadSnapshot(ctx) {
			return fmt.Errorf("disconnected while walking to (%.0f,%.0f)", tx, tz)
		}
		// Respawn if we died mid-walk (rare; a stray mob crossed the path).
		for _, ev := range b.DrainCombat() {
			if ev.EventType == "death" {
				_ = b.Respawn(ctx)
			}
		}
	}
}

// huntMob finds `count` living mobs of `mobDef` by orbiting the band anchors,
// engages each and waits for the kill (XP event), then returns. Death spots
// are recorded in res.KillSpots for later pick-up (collect objectives).
func huntMob(ctx context.Context, b *WorldBot, mobDef string, band, count int32, res *QuesterResult, dbg io.Writer, loopEnd time.Time) (int32, error) {
	anchors := bandAnchors[int(band)]
	if len(anchors) == 0 {
		return 0, fmt.Errorf("no anchors for band %d", band)
	}
	anchorIdx := 0
	var killed int32
	var engaged uint64
	var deathPos *aet.EntityState
	lastDebug := time.Now()
	for killed < count {
		if time.Now().After(loopEnd) {
			return killed, fmt.Errorf("hunt %s budget exceeded (%d/%d kills)", mobDef, killed, count)
		}
		if !readFrameBounded(ctx, b, 250*time.Millisecond) {
			return killed, fmt.Errorf("disconnected hunting %s", mobDef)
		}
		// Death → respawn at shrine and continue the hunt. Always respawn: the
		// death combat event can precede the hp=0 self snapshot by a tick, so a
		// "self still alive" guard here would strand the bot dead mid-hunt.
		// A duplicate death event only yields a harmless server "not dead".
		for _, ev := range b.DrainCombat() {
			if ev.EventType == "death" {
				_ = b.Respawn(ctx)
			}
			if ev.EventType == "xp" {
				killed++
				engaged = 0
				if deathPos != nil {
					res.KillSpots[mobDef] = append(res.KillSpots[mobDef], dropSpot{X: float64(deathPos.Position.X), Z: float64(deathPos.Position.Z)})
					if len(res.KillSpots[mobDef]) > 8 {
						res.KillSpots[mobDef] = res.KillSpots[mobDef][len(res.KillSpots[mobDef])-8:]
					}
					deathPos = nil
				}
				fmt.Fprintf(dbg, "quester: killed %s (%d/%d)\n", mobDef, killed, count)
			}
		}
		// Find the mob and engage it.
		target := b.FindMobByRef(mobDef)
		if time.Since(lastDebug) > 5*time.Second {
			lastDebug = time.Now()
			mobs := ""
			for _, e := range b.Seen {
				if e.EntityType == "mob" {
					mobs += fmt.Sprintf(" [%s hp%d @%.0f,%.0f]", e.Name, e.Hp, e.Position.X, e.Position.Z)
				}
			}
			fmt.Fprintf(dbg, "quester: hunt %s pos=(%.0f,%.0f) engaged=%d target=%v mobs:%s\n",
				mobDef, b.PosX, b.PosZ, engaged, target != nil, mobs)
		}
		if target != nil {
			deathPos = target
			dx := float64(target.Position.X) - b.PosX
			dz := float64(target.Position.Z) - b.PosZ
			// Engage at attack range (3m), not 4m: standing at 4m is outside
			// blade_strike's 3m radius so auto-attack lands nothing and the
			// fight stalemates. Re-issue the attack EVERY frame in range:
			// mobs respawn reusing the same entity id, so an engaged id that
			// stayed equal across a respawn would otherwise leave the server
			// auto-attack targeting the dead mob forever.
			if math.Hypot(dx, dz) > 3 {
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					return killed, err
				}
			} else {
				engaged = target.EntityId
				_ = b.Stop(ctx)
				_ = b.AutoAttack(ctx, target.EntityId, true)
			}
			continue
		}
		// No mob in AOI: walk to the current anchor, only advancing to the
		// next once we're near it (cycling every frame would jitter in place).
		if engaged != 0 {
			_ = b.AutoAttack(ctx, 0, false)
			engaged = 0
		}
		a := anchors[anchorIdx%len(anchors)]
		ax, az := a[0], a[1]
		if math.Hypot(ax-b.PosX, az-b.PosZ) < 6 {
			anchorIdx++
			a = anchors[anchorIdx%len(anchors)]
			ax, az = a[0], a[1]
		}
		if err := b.steer(ctx, ax, az); err != nil {
			return killed, err
		}
	}
	return killed, nil
}

// collectDrops walks onto `count` ground drops of `itemDef` and picks them up.
// Drops spawn where their source mob died (res.KillSpots), so the bot heads
// straight to recorded death spots rather than wandering.
func collectDrops(ctx context.Context, b *WorldBot, itemDef, sourceMob string, count int32, res *QuesterResult, dbg io.Writer, loopEnd time.Time) (int32, error) {
	var got int32
	deadline := time.Now().Add(2 * time.Minute)
	spots := res.KillSpots[sourceMob]
	spotsChecked := make([]bool, len(spots))
	for got < count {
		if time.Now().After(loopEnd) {
			return got, fmt.Errorf("collect %s budget exceeded (%d/%d)", itemDef, got, count)
		}
		if time.Now().After(deadline) && got == 0 {
			return got, fmt.Errorf("collect %s timed out before any drop", itemDef)
		}
		if !readFrameBounded(ctx, b, 250*time.Millisecond) {
			return got, fmt.Errorf("disconnected collecting %s", itemDef)
		}
		for _, le := range b.DrainLoot() {
			if le.Ok && le.ItemDefId == itemDef {
				got++
				fmt.Fprintf(dbg, "quester: collected %s (%d/%d)\n", itemDef, got, count)
			}
		}
		// Look for a drop of the right def on the ground.
		if d := findDropByDef(b, itemDef); d != nil {
			dx := float64(d.Position.X) - b.PosX
			dz := float64(d.Position.Z) - b.PosZ
			if math.Hypot(dx, dz) <= 2.8 {
				_ = b.PickupItem(ctx, d.EntityId)
			} else if err := b.steer(ctx, float64(d.Position.X), float64(d.Position.Z)); err != nil {
				return got, err
			}
		} else if dx, dz, ok := nearestUnvisitedSpot(b, spots, spotsChecked); ok {
			// No drop in AOI: go to the next unvisited death spot (its drop
			// spawns there and lives for a couple minutes).
			if err := b.steer(ctx, dx, dz); err != nil {
				return got, err
			}
		} else {
			// No recorded spots left: fall back to orbiting the band anchors.
			a := bandAnchors[1][int(got+int32(time.Now().Unix()))%len(bandAnchors[1])]
			if err := b.steer(ctx, a[0], a[1]); err != nil {
				return got, err
			}
		}
	}
	return got, nil
}

// nearestUnvisitedSpot returns the nearest unvisited death spot and marks it
// visited once the bot is close enough to have checked it.
func nearestUnvisitedSpot(b *WorldBot, spots []dropSpot, checked []bool) (float64, float64, bool) {
	best := -1
	bestDist := math.MaxFloat64
	for i, s := range spots {
		if checked[i] {
			continue
		}
		d := (s.X-b.PosX)*(s.X-b.PosX) + (s.Z-b.PosZ)*(s.Z-b.PosZ)
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	if bestDist < 3*3 {
		checked[best] = true
	}
	return spots[best].X, spots[best].Z, true
}

// findDropByDef returns the nearest ground drop of an item def in AOI.
func findDropByDef(b *WorldBot, itemDef string) *aet.EntityState {
	var best *aet.EntityState
	var bestDist float64
	for _, e := range b.Seen {
		if e.EntityType != "drop" || e.RefId != itemDef {
			continue
		}
		dx := float64(e.Position.X) - b.PosX
		dz := float64(e.Position.Z) - b.PosZ
		d := dx*dx + dz*dz
		if best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	return best
}

// readFrameBounded reads one frame with a bounded timeout (any payload), so
// a quiet world — the server only emits snapshots on change — can't stall the
// caller. Returns false only on a real connection error; a timeout is fine.
func readFrameBounded(ctx context.Context, b *WorldBot, d time.Duration) bool {
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	if _, err := b.ReadFrame(rctx); err != nil {
		return errors.Is(err, context.DeadlineExceeded)
	}
	return true
}

// waitDialog drains frames until an NpcDialogEvent for `npcID` arrives.
// Frames are read with a bounded per-iteration deadline so an idle world (the
// server only emits snapshots on change) can't stall the wait.
func waitDialog(ctx context.Context, b *WorldBot, npcID string) *aet.NpcDialogEvent {
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, d := range b.DrainDialogs() {
			if d.NpcId == npcID {
				return d
			}
		}
		if time.Now().After(deadline) {
			return nil
		}
		rctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		_, err := b.ReadFrame(rctx)
		cancel()
		if err != nil {
			if rctx.Err() != nil && time.Now().Before(deadline) {
				continue
			}
			return nil
		}
	}
}

// questError returns the server's rejection reason for a quest, if the bot
// has buffered an Ok=false QuestEvent for it (e.g. a rejected turn-in).
func questError(b *WorldBot, questID string) string {
	for _, ev := range b.DrainQuests() {
		if ev.QuestId == questID && !ev.Ok && ev.Error != "" {
			return ev.Error
		}
	}
	return ""
}

// waitQuestEvent drains frames until a QuestEvent for `questID` in `state`
// arrives (objective-progress events with the same id are skipped).
func waitQuestEvent(ctx context.Context, b *WorldBot, questID, state string) *aet.QuestEvent {
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, ev := range b.DrainQuests() {
			if ev.QuestId == questID && (state == "" || ev.State == state) {
				return ev
			}
		}
		if time.Now().After(deadline) {
			return nil
		}
		rctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		_, err := b.ReadFrame(rctx)
		cancel()
		if err != nil {
			if rctx.Err() != nil && time.Now().Before(deadline) {
				continue
			}
			return nil
		}
	}
}
