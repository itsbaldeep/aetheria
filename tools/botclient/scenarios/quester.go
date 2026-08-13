// Scenario: M5 acceptance — a bot plays the full Havenport quest chain (brief
// §141): talks to the quest giver, accepts quests, kills quest mobs, collects
// quest drops, buys gear from the vendor, turns in each quest, and levels
// from 1 to ~9. It drives the real QuestAccept/NpcInteract/QuestTurnIn wire
// messages and asserts every quest in the 15-quest chain reaches complete.
package scenarios

import (
	"context"
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
}

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
	Talk         string // npc def to talk to ("" for none; interact always happens)
}

var questChain = []questStep{
	{ID: "q_welcome", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", Talk: "aldric_questgiver"},
	{ID: "q_boar_pests", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "forest_boar", Band: 1},
	{ID: "q_hide_collector", Giver: "aldric_questgiver", Turnin: "vendor_maren", Collect: "boar_hide"},
	{ID: "q_wolf_threat", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ember_wolf", Band: 1},
	{ID: "q_rat_plague", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "cinder_rat", Band: 1},
	{ID: "q_thorn_clearing", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "thorn_viper", Band: 1},
	{ID: "q_gear_up", Giver: "aldric_questgiver", Turnin: "vendor_maren", Collect: "iron_sword", CollectIsBuy: true, Talk: "vendor_maren"},
	{ID: "q_hound_hunt", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ember_hound", Band: 2},
	{ID: "q_brute_force", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ash_brute", Band: 2},
	{ID: "q_wraith_warden", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "cinder_wraith", Band: 2},
	{ID: "q_magma_stopper", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "magma_boar", Band: 2},
	{ID: "q_lava_crawler_clear", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "lava_crawler", Band: 3},
	{ID: "q_ashmaw_fang", Giver: "aldric_questgiver", Turnin: "aldric_questgiver", MobDef: "ashmaw", Band: 3, Collect: "ashmaw_fang"},
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

	res := &QuesterResult{QuestsCompleted: map[string]bool{}}
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
	waitQuestEvent(ctx, b, step.ID)

	// 2. Advance every objective.
	if step.MobDef != "" {
		kills, err := huntMob(ctx, b, step.MobDef, int32(step.Band), 3, dbg, loopEnd)
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
			n, err := collectDrops(ctx, b, step.Collect, 3, dbg, loopEnd)
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
	if ev := waitQuestEvent(ctx, b, step.ID); ev == nil || ev.State != "complete" {
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
// engages each and waits for the kill (XP event), then returns.
func huntMob(ctx context.Context, b *WorldBot, mobDef string, band, count int32, dbg io.Writer, loopEnd time.Time) (int32, error) {
	anchors := bandAnchors[int(band)]
	if len(anchors) == 0 {
		return 0, fmt.Errorf("no anchors for band %d", band)
	}
	anchorIdx := 0
	var killed int32
	var engaged uint64
	for killed < count {
		if time.Now().After(loopEnd) {
			return killed, fmt.Errorf("hunt %s budget exceeded (%d/%d kills)", mobDef, killed, count)
		}
		if !b.ReadSnapshot(ctx) {
			return killed, fmt.Errorf("disconnected hunting %s", mobDef)
		}
		// Death → respawn at shrine and continue the hunt.
		for _, ev := range b.DrainCombat() {
			if ev.EventType == "death" {
				_ = b.Respawn(ctx)
			}
			if ev.EventType == "xp" {
				killed++
				engaged = 0
				fmt.Fprintf(dbg, "quester: killed %s (%d/%d)\n", mobDef, killed, count)
			}
		}
		// Find the mob and engage it.
		target := b.FindMobByRef(mobDef)
		if target != nil {
			dx := float64(target.Position.X) - b.PosX
			dz := float64(target.Position.Z) - b.PosZ
			if math.Hypot(dx, dz) > 4 {
				if err := b.steer(ctx, float64(target.Position.X), float64(target.Position.Z)); err != nil {
					return killed, err
				}
			} else if engaged != target.EntityId {
				engaged = target.EntityId
				_ = b.Stop(ctx)
				_ = b.AutoAttack(ctx, target.EntityId, true)
			}
			continue
		}
		// No mob in AOI: orbit the next anchor.
		if engaged != 0 {
			_ = b.AutoAttack(ctx, 0, false)
			engaged = 0
		}
		a := anchors[anchorIdx%len(anchors)]
		anchorIdx++
		if err := b.steer(ctx, a[0], a[1]); err != nil {
			return killed, err
		}
	}
	return killed, nil
}

// collectDrops walks onto `count` ground drops of `itemDef` and picks them up.
func collectDrops(ctx context.Context, b *WorldBot, itemDef string, count int32, dbg io.Writer, loopEnd time.Time) (int32, error) {
	var got int32
	deadline := time.Now().Add(2 * time.Minute)
	for got < count {
		if time.Now().After(loopEnd) {
			return got, fmt.Errorf("collect %s budget exceeded (%d/%d)", itemDef, got, count)
		}
		if time.Now().After(deadline) && got == 0 {
			return got, fmt.Errorf("collect %s timed out before any drop", itemDef)
		}
		if !b.ReadSnapshot(ctx) {
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
		} else {
			// Orbit the band-1 anchors: quest collectables (boar hide) drop
			// from band-1 mobs we already killed.
			a := bandAnchors[1][int(got+int32(time.Now().Unix()))%len(bandAnchors[1])]
			if err := b.steer(ctx, a[0], a[1]); err != nil {
				return got, err
			}
		}
	}
	return got, nil
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

// waitDialog drains frames until an NpcDialogEvent for `npcID` arrives.
func waitDialog(ctx context.Context, b *WorldBot, npcID string) *aet.NpcDialogEvent {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, d := range b.DrainDialogs() {
			if d.NpcId == npcID {
				return d
			}
		}
		if !b.ReadSnapshot(ctx) {
			return nil
		}
	}
	return nil
}

// waitQuestEvent drains frames until a QuestEvent for `questID` arrives.
func waitQuestEvent(ctx context.Context, b *WorldBot, questID string) *aet.QuestEvent {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range b.DrainQuests() {
			if ev.QuestId == questID {
				return ev
			}
		}
		if !b.ReadSnapshot(ctx) {
			return nil
		}
	}
	return nil
}
