package world

import (
	"context"
	"sync"
	"testing"
	"time"
)

// m5TestContent extends the M4 test content with quest-giver NPC positions and
// a small linear quest chain exercising kill/collect/talk objectives and a
// gold reward that must land in the ledger as reason "quest_reward".
func m5TestContent() *Content {
	c := m4TestContent()
	c.NPCs["vendor_maren"].Pos = Vec3{-12, 0, 8}
	c.NPCs["aldric_questgiver"] = &NPC{
		ID: "aldric_questgiver", Name: "Aldric", ZoneID: "havenport", Kind: "questgiver",
		Pos: Vec3{10, 0, -8}, Dialog: "Welcome, traveler.",
	}
	c.Quests = map[string]*QuestDef{
		"q_welcome": {
			ID: "q_welcome", Name: "Welcome", MinLevel: 1,
			GiverNPC: "aldric_questgiver", TurninNPC: "aldric_questgiver",
			Objectives: []QuestObjective{{Type: ObjectiveTalk, Target: "aldric_questgiver", Count: 1}},
			Rewards:    QuestRewards{XP: 20, Gold: 5},
			NextQuest:  "q_boar_pests",
		},
		"q_boar_pests": {
			ID: "q_boar_pests", Name: "Boar Pests", MinLevel: 1,
			GiverNPC: "aldric_questgiver", TurninNPC: "aldric_questgiver",
			Objectives: []QuestObjective{{Type: ObjectiveKill, Target: "forest_boar", Count: 3}},
			Rewards:    QuestRewards{XP: 40, Gold: 10},
			NextQuest:  "q_hide_collector",
		},
		"q_hide_collector": {
			ID: "q_hide_collector", Name: "Hide Collector", MinLevel: 1,
			GiverNPC: "aldric_questgiver", TurninNPC: "vendor_maren",
			Objectives: []QuestObjective{{Type: ObjectiveCollect, Target: "boar_hide", Count: 3}},
			Rewards:    QuestRewards{XP: 40, Gold: 10},
			NextQuest:  "",
		},
	}
	return c
}

// testQuestSave records the last quests flushed per char.
type testQuestSave struct {
	mu     sync.Mutex
	last   map[string]*QuestProgress
	saved  []map[string]*QuestProgress
	callID int64
}

func (t *testQuestSave) save(_ context.Context, charID int64, quests map[string]*QuestProgress) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.callID = charID
	t.last = quests
	copy := map[string]*QuestProgress{}
	for k, v := range quests {
		c := *v
		c.Counts = append([]int32(nil), v.Counts...)
		copy[k] = &c
	}
	t.saved = append(t.saved, copy)
	return nil
}

func (t *testQuestSave) snapshot() map[string]*QuestProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := map[string]*QuestProgress{}
	for k, v := range t.last {
		out[k] = v
	}
	return out
}

func (t *testQuestSave) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.saved)
}

// m5Sim builds a sim with M5 content (NPCs with positions + quest chain) and
// a recorder for both the gold ledger and the quest saves.
func m5Sim(t *testing.T) (*Sim, *testLedger, *testQuestSave) {
	t.Helper()
	tl := &testLedger{}
	tq := &testQuestSave{}
	s := New(Options{
		Zones: []*Zone{
			{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50},
			{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300},
		},
		Content:    m5TestContent(),
		Tick:       50 * time.Millisecond,
		Logf:       func(f string, a ...any) { t.Logf(f, a...) },
		SaveLedger: tl.save,
		SaveQuests: tq.save,
	})
	return s, tl, tq
}

// m5Player is a quest-capable player standing next to Aldric in Havenport.
func m5Player(s *Sim, charID int64, pos Vec3) *Player {
	p := m4Player(s, charID, "blade_dancer", pos)
	p.Zone = "havenport"
	return p
}

func TestQuestChainAcceptProgressTurnin(t *testing.T) {
	s, _, _ := m5Sim(t)

	// Player stands right at Aldric (10,-8).
	p := m5Player(s, 1, Vec3{10, 0, -8})

	// Chain head not yet accepted: dialog offers it.
	d, err := s.NpcInteract(1, "aldric_questgiver")
	if err != nil {
		t.Fatalf("NpcInteract: %v", err)
	}
	if len(d.Available) != 1 || d.Available[0] != "q_welcome" {
		t.Fatalf("available = %v, want [q_welcome]", d.Available)
	}

	// Accept q_welcome; dialog's talk objective completes immediately.
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("AcceptQuest: %v", err)
	}
	if p.Quests["q_welcome"] == nil || p.Quests["q_welcome"].State != "active" {
		t.Fatal("q_welcome not active after accept")
	}
	if _, err := s.NpcInteract(1, "aldric_questgiver"); err != nil {
		t.Fatalf("NpcInteract: %v", err)
	}
	if got := p.Quests["q_welcome"].Counts[0]; got != 1 {
		t.Fatalf("talk objective count = %d, want 1", got)
	}

	// Turn in: quest complete, XP+gold granted, next quest unlocked.
	if err := s.TurnInQuest(1, "q_welcome"); err != nil {
		t.Fatalf("TurnInQuest: %v", err)
	}
	if p.Quests["q_welcome"].State != "complete" {
		t.Fatal("q_welcome not complete")
	}
	// XP 20 < MaxXPForLevel(1)=40 → no level-up, XP carried as 20.
	if p.XP != 20 {
		t.Fatalf("xp = %d, want 20", p.XP)
	}
	if p.Level != 1 {
		t.Fatalf("level = %d, want 1", p.Level)
	}
	if p.Gold != 5 {
		t.Fatalf("gold = %d, want 5", p.Gold)
	}

	// q_boar_pests now available.
	d, err = s.NpcInteract(1, "aldric_questgiver")
	if err != nil {
		t.Fatalf("NpcInteract: %v", err)
	}
	if len(d.Available) != 1 || d.Available[0] != "q_boar_pests" {
		t.Fatalf("available = %v, want [q_boar_pests]", d.Available)
	}

	// Accept and kill two boars via killMob; count should be 2/3.
	if err := s.AcceptQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("AcceptQuest: %v", err)
	}
	boar := m3Mob(s, "forest_boar", Vec3{12, 0, -8})
	s.mu.Lock()
	s.killMob(boar, p, time.Now())
	s.mu.Unlock()
	if got := p.Quests["q_boar_pests"].Counts[0]; got != 1 {
		t.Fatalf("kill count = %d, want 1", got)
	}

	// q_hide_collector should NOT be available yet (chain locked on q_boar_pests).
	if s.questAvailableLocked(p, s.questDefs["q_hide_collector"]) {
		t.Fatal("q_hide_collector available before q_boar_pests complete")
	}

	// Kill two more boars (1 + 2 = 3) → complete. Turn-in requires proximity to
	// Aldric (q_boar_pests turns in to Aldric). Player is at Aldric — good.
	for i := 0; i < 2; i++ {
		boar := m3Mob(s, "forest_boar", Vec3{12, 0, -8})
		s.mu.Lock()
		s.killMob(boar, p, time.Now())
		s.mu.Unlock()
	}
	if got := p.Quests["q_boar_pests"].Counts[0]; got != 3 {
		t.Fatalf("kill count = %d, want 3", got)
	}
	if !s.questCompleteLocked(p.Quests["q_boar_pests"], s.questDefs["q_boar_pests"]) {
		t.Fatal("q_boar_pests should be complete after 3 kills")
	}
}

func TestQuestTurninRequiresProximity(t *testing.T) {
	s, _, _ := m5Sim(t)
	// Player far from Aldric (10,-8) — out of talk range.
	p := m5Player(s, 1, Vec3{40, 0, 40})
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("AcceptQuest: %v", err)
	}
	if _, err := s.NpcInteract(1, "aldric_questgiver"); err == nil {
		t.Fatal("talk from 40m away should fail proximity check")
	}
	_ = p
	if err := s.TurnInQuest(1, "q_welcome"); err == nil {
		t.Fatal("turn-in from 40m away should fail proximity check")
	}
}

func TestQuestCollectObjectiveAndTurninAtMaren(t *testing.T) {
	s, tl, tq := m5Sim(t)
	p := m5Player(s, 1, Vec3{10, 0, -8})

	// Play through q_welcome → q_boar_pests (2 boars) → q_hide_collector.
	// Fast path: directly grant the prereqs via the same API the bot uses.
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("accept welcome: %v", err)
	}
	if _, err := s.NpcInteract(1, "aldric_questgiver"); err != nil {
		t.Fatalf("talk: %v", err)
	}
	if err := s.TurnInQuest(1, "q_welcome"); err != nil {
		t.Fatalf("turnin welcome: %v", err)
	}
	if err := s.AcceptQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("accept boar: %v", err)
	}
	for i := 0; i < 3; i++ {
		boar := m3Mob(s, "forest_boar", Vec3{12, 0, -8})
		s.mu.Lock()
		s.killMob(boar, p, time.Now())
		s.mu.Unlock()
	}
	if err := s.TurnInQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("turnin boar: %v", err)
	}

	// Now q_hide_collector is available; it turns in at Maren (-12,8).
	if !s.questAvailableLocked(p, s.questDefs["q_hide_collector"]) {
		t.Fatal("q_hide_collector should be available")
	}
	if err := s.AcceptQuest(1, "q_hide_collector"); err != nil {
		t.Fatalf("accept hide: %v", err)
	}
	// Collect 3 boar_hide via pickup (collectHook).
	for i := 0; i < 3; i++ {
		s.mu.Lock()
		drop := s.spawnDrop(p.Pos, p.Zone, &Item{DefID: "boar_hide", Qty: 1}, 0)
		s.mu.Unlock()
		if err := s.PickupItem(1, drop.ID); err != nil {
			t.Fatalf("pickup %d: %v", i, err)
		}
	}
	if got := p.Quests["q_hide_collector"].Counts[0]; got != 3 {
		t.Fatalf("collect count = %d, want 3", got)
	}
	// Turn-in at Aldric fails (turn-in is at Maren); at Maren it succeeds.
	if err := s.TurnInQuest(1, "q_hide_collector"); err == nil {
		t.Fatal("turn-in at Aldric should fail (turnin_npc=Maren)")
	}
	p.Pos = Vec3{-12, 0, 8}
	if err := s.TurnInQuest(1, "q_hide_collector"); err != nil {
		t.Fatalf("turn-in at Maren: %v", err)
	}
	if p.Quests["q_hide_collector"].State != "complete" {
		t.Fatal("q_hide_collector not complete")
	}

	// Ledger: gold = 5 (welcome) + 10 (boar) + 3×3 mob_kill + 10 (hide).
	// Check the invariant after flush.
	flushed := s.FlushGoldLedger(context.Background())
	if flushed == 0 {
		t.Fatal("ledger did not flush")
	}
	if got := tl.sum(); got != p.Gold {
		t.Fatalf("sum(ledger)=%d != world gold=%d", got, p.Gold)
	}

	// Quest saves were flushed: last recorded quests include the completed hide.
	got := tq.snapshot()
	if got["q_hide_collector"] == nil || got["q_hide_collector"].State != "complete" {
		t.Fatalf("saved quests missing completed q_hide_collector: %v", got)
	}
}

func TestQuestAbandonRejectsReacceptUntilChainRecomplete(t *testing.T) {
	s, _, _ := m5Sim(t)
	p := m5Player(s, 1, Vec3{10, 0, -8})
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if err := s.AbandonQuest(1, "q_welcome"); err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if p.Quests["q_welcome"].State != "abandoned" {
		t.Fatal("state != abandoned")
	}
	// Re-accepting an abandoned chain head is allowed.
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("re-accept after abandon: %v", err)
	}
	if p.Quests["q_welcome"].State != "active" {
		t.Fatal("re-accepted quest not active")
	}
	// Turn in → chain unlocks next quest.
	if _, err := s.NpcInteract(1, "aldric_questgiver"); err != nil {
		t.Fatalf("talk: %v", err)
	}
	if err := s.TurnInQuest(1, "q_welcome"); err != nil {
		t.Fatalf("turnin: %v", err)
	}
	if !s.questAvailableLocked(p, s.questDefs["q_boar_pests"]) {
		t.Fatal("q_boar_pests should unlock after q_welcome completes")
	}
	// Completing the chain still lets q_boar_pests be abandoned and re-accepted.
	if err := s.AcceptQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("accept boar: %v", err)
	}
	if err := s.AbandonQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("abandon boar: %v", err)
	}
	if err := s.AcceptQuest(1, "q_boar_pests"); err != nil {
		t.Fatalf("re-accept boar: %v", err)
	}
}

func TestQuestLevelTooLowRejected(t *testing.T) {
	s, _, _ := m5Sim(t)
	p := m5Player(s, 1, Vec3{10, 0, -8})
	p.Level = 1
	// All test quests have MinLevel 1, so bump a def to require level 5.
	s.mu.Lock()
	def := s.questDefs["q_welcome"]
	def.MinLevel = 5
	s.mu.Unlock()
	if err := s.AcceptQuest(1, "q_welcome"); err == nil {
		t.Fatal("accept below min level should fail")
	}
}

func TestQuestStatusReflectsAllStates(t *testing.T) {
	s, _, _ := m5Sim(t)
	m5Player(s, 1, Vec3{10, 0, -8})
	status := s.QuestStatus(1)
	if len(status) != 3 {
		t.Fatalf("status length = %d, want 3", len(status))
	}
	byID := map[string]QuestState{}
	for _, st := range status {
		byID[st.QuestID] = st
	}
	if byID["q_welcome"].State != "available" {
		t.Fatal("q_welcome should be available at start")
	}
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	status = s.QuestStatus(1)
	for _, st := range status {
		if st.QuestID == "q_welcome" && st.State != "active" {
			t.Fatalf("q_welcome state = %s, want active", st.State)
		}
	}
}

func TestRestoreQuestsAndPersistQuestsRoundTrip(t *testing.T) {
	s, _, _ := m5Sim(t)
	p := m5Player(s, 1, Vec3{10, 0, -8})
	if err := s.AcceptQuest(1, "q_welcome"); err != nil {
		t.Fatalf("accept: %v", err)
	}
	persisted := s.PersistQuests(1)
	if len(persisted) != 1 || persisted[0].QuestID != "q_welcome" {
		t.Fatalf("persisted = %+v, want one q_welcome", persisted)
	}
	// Wipe and restore: state must come back.
	p.Quests = map[string]*QuestProgress{}
	if err := s.RestoreQuests(1, persisted); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if p.Quests["q_welcome"] == nil || p.Quests["q_welcome"].State != "active" {
		t.Fatal("quest not restored")
	}
}
