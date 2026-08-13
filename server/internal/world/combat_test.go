package world

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// m3TestContent builds a small Content set covering the skill kinds and a
// couple of mobs so tests don't depend on the real seed files.
func m3TestContent() *Content {
	return &Content{
		Skills: map[string]*SkillDef{
			"blade_strike": {ID: "blade_strike", Name: "Strike", Class: "blade_dancer", Kind: SkillAuto, Range: 3, Power: 10},
			"whirlwind":    {ID: "whirlwind", Name: "Whirlwind", Class: "blade_dancer", Kind: SkillPBAOE, Radius: 6, CooldownMS: 8000, CostMP: 15, Power: 14},
			"charge":       {ID: "charge", Name: "Charge", Class: "blade_dancer", Kind: SkillTabTarget, Range: 15, CooldownMS: 10000, CostMP: 10, Power: 20},
			"rend":         {ID: "rend", Name: "Rend", Class: "blade_dancer", Kind: SkillTabTarget, Range: 4, CooldownMS: 6000, CostMP: 8, Power: 18},
			"execute":      {ID: "execute", Name: "Execute", Class: "blade_dancer", Kind: SkillTabTarget, Range: 4, CooldownMS: 12000, CostMP: 15, Power: 60},
			"twin_slash":   {ID: "twin_slash", Name: "Twin Slash", Class: "blade_dancer", Kind: SkillTabTarget, Range: 4, CooldownMS: 3000, CostMP: 5, Power: 12},
			"arcane_bolt":  {ID: "arcane_bolt", Name: "Bolt", Class: "spellweaver", Kind: SkillAuto, Range: 20, Power: 8},
			"fireball":     {ID: "fireball", Name: "Fireball", Class: "spellweaver", Kind: SkillAimed, Range: 22, Radius: 5, CooldownMS: 4000, CostMP: 10, Power: 16},
			"mana_shield":  {ID: "mana_shield", Name: "Shield", Class: "spellweaver", Kind: SkillSelf, CooldownMS: 20000, CostMP: 20, Power: 30},
			"mob_bite":     {ID: "mob_bite", Name: "Bite", Class: "mob", Kind: SkillAuto, Range: 2, CooldownMS: 1500, Power: 5},
			"mob_gore":     {ID: "mob_gore", Name: "Gore", Class: "mob", Kind: SkillTabTarget, Range: 2, CooldownMS: 5000, Power: 14},
		},
		Mobs: map[string]*MobDef{
			"forest_boar": {ID: "forest_boar", Name: "Forest Boar", Level: 1, HP: 50, ZoneID: "emberfield", AggroRadius: 8, LeashRadius: 20, Skills: []string{"mob_bite", "mob_gore"}, XPReward: 20, SpawnBand: 1},
			"ashmaw":      {ID: "ashmaw", Name: "Ashmaw", Level: 6, HP: 420, ZoneID: "emberfield", AggroRadius: 12, LeashRadius: 30, Skills: []string{"mob_gore", "mob_bite"}, XPReward: 220, SpawnBand: 3},
		},
		Zones: map[string]*ZoneContent{
			"emberfield": {ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300, Shrine: Vec3{150, 0, 0}},
		},
	}
}

// m3Sim builds a sim with test content and a fast tick, with SaveChar captured.
func m3Sim(t *testing.T) (*Sim, *testSave) {
	t.Helper()
	ts := &testSave{}
	s := New(Options{
		Zones:    []*Zone{{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300}},
		Content:  m3TestContent(),
		Tick:     50 * time.Millisecond,
		Logf:     func(f string, a ...any) { t.Logf(f, a...) },
		SaveChar: ts.saveChar,
	})
	return s, ts
}

type testSave struct {
	level int32
	xp    int64
	hp    int64
	mp    int64
	calls int
}

func (t *testSave) saveChar(_ context.Context, _ int64, level int32, xp, hp, mp int64) error {
	t.level, t.xp, t.hp, t.mp = level, xp, hp, mp
	t.calls++
	return nil
}

func m3Player(s *Sim, charID int64, class string, pos Vec3) *Player {
	p := &Player{
		Entity:      Entity{Type: TypePlayer, Name: "P", Zone: "emberfield", Pos: pos, MaxHP: 100, HP: 100, Level: 1},
		CharacterID: charID,
		Class:       class,
		MaxSpeed:    8,
		MP:          100,
		MaxMP:       100,
		cooldowns:   map[string]int64{},
		Outbox:      s.NewPlayerOutbox(),
	}
	p.Ready.Store(true)
	if err := s.Spawn(p); err != nil {
		panic(err)
	}
	return p
}

func m3Mob(s *Sim, defID string, pos Vec3) *Mob {
	s.mu.Lock()
	defer s.mu.Unlock()
	def := s.mobDefs[defID]
	if def == nil {
		panic("no def " + defID)
	}
	s.nextID++
	m := NewMob(def, s.nextID, pos)
	s.mobs[m.ID] = m
	s.grid.Insert(&m.Entity)
	return m
}

// readCombatEvents collects CombatEvent frames from a player's outbox.
func readCombatEvents(t *testing.T, p *Player) []*aet.CombatEvent {
	t.Helper()
	var out []*aet.CombatEvent
	deadline := time.After(200 * time.Millisecond)
	for {
		select {
		case frame := <-p.Outbox:
			env := &aet.Envelope{}
			if proto.Unmarshal(frame, env) != nil {
				continue
			}
			if env.PayloadType != "aetheria.CombatEvent" {
				continue
			}
			ev := &aet.CombatEvent{}
			if proto.Unmarshal(env.Payload, ev) == nil {
				out = append(out, ev)
			}
		case <-deadline:
			return out
		}
	}
}

func TestSkillWrongClass(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	if err := s.CastSkill(1, CastRequest{SkillID: "arcane_bolt", TargetID: 0}); !errors.Is(err, ErrSkillWrongClass) {
		t.Fatalf("wrong class = %v, want ErrSkillWrongClass", err)
	}
}

func TestSkillUnknown(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	if err := s.CastSkill(1, CastRequest{SkillID: "nope"}); !errors.Is(err, ErrSkillUnknown) {
		t.Fatalf("unknown skill = %v", err)
	}
}

func TestSkillOutOfRange(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	m := m3Mob(s, "forest_boar", Vec3{50, 0, 0}) // way out of melee range
	if err := s.CastSkill(1, CastRequest{SkillID: "rend", TargetID: m.ID}); !errors.Is(err, ErrOutOfRange) {
		t.Fatalf("out of range = %v, want ErrOutOfRange", err)
	}
}

func TestSkillCooldown(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	m := m3Mob(s, "forest_boar", Vec3{2, 0, 0})
	// First cast lands (mob in range, MP sufficient).
	if err := s.CastSkill(1, CastRequest{SkillID: "rend", TargetID: m.ID}); err != nil {
		t.Fatalf("first cast: %v", err)
	}
	// Second cast within 6 s cooldown must be rejected.
	if err := s.CastSkill(1, CastRequest{SkillID: "rend", TargetID: m.ID}); !errors.Is(err, ErrCooldown) {
		t.Fatalf("cooldown = %v, want ErrCooldown", err)
	}
}

func TestSkillCost(t *testing.T) {
	s, _ := m3Sim(t)
	p := m3Player(s, 1, "spellweaver", Vec3{0, 0, 0})
	p.MP = 0
	m := m3Mob(s, "forest_boar", Vec3{2, 0, 0})
	if err := s.CastSkill(1, CastRequest{SkillID: "fireball", AimPos: &Vec3{2, 0, 0}, TargetID: m.ID}); !errors.Is(err, ErrNoMP) {
		t.Fatalf("cost = %v, want ErrNoMP", err)
	}
}

func TestSkillSafeZone(t *testing.T) {
	safe := New(Options{
		Zones:   []*Zone{{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50}},
		Content: m3TestContent(),
	})
	p := &Player{Entity: Entity{Type: TypePlayer, Name: "P", Zone: "havenport", Pos: Vec3{0, 0, 0}, MaxHP: 100, HP: 100}, CharacterID: 1, Class: "blade_dancer", MP: 100, MaxMP: 100}
	p.Ready.Store(true)
	if err := safe.Spawn(p); err != nil {
		t.Fatal(err)
	}
	m3Mob(safe, "forest_boar", Vec3{2, 0, 0})
	if err := safe.CastSkill(1, CastRequest{SkillID: "rend", TargetID: 2}); !errors.Is(err, ErrSafeZone) {
		t.Fatalf("safe zone = %v, want ErrSafeZone", err)
	}
}

func TestAutoAttackKillsBoarAndGrantsXP(t *testing.T) {
	s, ts := m3Sim(t)
	p := m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	m := m3Mob(s, "forest_boar", Vec3{2, 0, 0})
	if err := s.SetAutoAttack(1, m.ID, true); err != nil {
		t.Fatal(err)
	}
	// Drive the sim until the boar dies (50 HP / ~10 dmg per auto ≈ 5+ hits).
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.tickOnce(time.Now())
		s.mu.Lock()
		dead := !m.alive
		s.mu.Unlock()
		if dead {
			break
		}
		if time.Now().After(deadline) {
			s.mu.Lock()
			hp := m.HP
			s.mu.Unlock()
			t.Fatalf("boar never died (hp=%d)", hp)
		}
	}
	s.mu.Lock()
	alive := m.alive
	s.mu.Unlock()
	if alive {
		t.Fatal("mob still alive")
	}
	// XP granted and persisted.
	if ts.xp != 20 {
		t.Fatalf("xp = %d, want 20", ts.xp)
	}
	if ts.calls == 0 {
		t.Fatal("SaveChar never called")
	}
	// Combat events include a kill + xp.
	evs := readCombatEvents(t, p)
	seen := map[string]bool{}
	for _, e := range evs {
		seen[e.EventType] = true
	}
	if !seen["kill"] || !seen["xp"] {
		t.Fatalf("missing kill/xp events, got %v", seen)
	}
}

func TestPlayerDiesToAshmawAndRespawns(t *testing.T) {
	s, _ := m3Sim(t)
	p := m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	m3Mob(s, "ashmaw", Vec3{2, 0, 0}) // level 6, 420 HP — will shred a level 1
	// Drive the sim: Ashmaw aggroes the player and kills it.
	deadline := time.Now().Add(20 * time.Second)
	for {
		s.tickOnce(time.Now())
		s.mu.Lock()
		dead := p.HP <= 0
		s.mu.Unlock()
		if dead {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("player never died")
		}
	}
	s.mu.Lock()
	gridOut := p.Entity.cell == nil
	s.mu.Unlock()
	if !gridOut {
		t.Fatal("dead player still in AOI grid")
	}
	// Respawn: HP restored, positioned at shrine, back in grid.
	if err := s.RespawnPlayer(1); err != nil {
		t.Fatalf("respawn: %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.HP != p.MaxHP {
		t.Fatalf("hp after respawn = %d, want %d", p.HP, p.MaxHP)
	}
	if p.Pos.X != 150 {
		t.Fatalf("respawn pos x = %v, want shrine 150", p.Pos.X)
	}
	if s.byEntity[p.ID] == nil {
		t.Fatal("respawned player not in grid")
	}
}

func TestAreaAimHitsOnlyTemplate(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "spellweaver", Vec3{0, 0, 0})
	// Two mobs: one inside the fireball radius, one just outside.
	inMob := m3Mob(s, "forest_boar", Vec3{4, 0, 0}) // inside radius 5
	outMob := m3Mob(s, "forest_boar", Vec3{50, 0, 0})
	aim := Vec3{0, 0, 0}
	if err := s.CastSkill(1, CastRequest{SkillID: "fireball", AimPos: &aim}); err != nil {
		t.Fatalf("cast: %v", err)
	}
	s.mu.Lock()
	inHP := inMob.HP
	outHP := outMob.HP
	inAlive := inMob.alive
	s.mu.Unlock()
	if inHP >= inMob.MaxHP {
		t.Fatalf("in-template mob took no damage (hp=%d/%d)", inHP, inMob.MaxHP)
	}
	if !inAlive {
		t.Fatal("in-template mob died from one fireball?")
	}
	if outHP != outMob.MaxHP {
		t.Fatalf("out-of-template mob took damage (hp=%d/%d)", outHP, outMob.MaxHP)
	}
}

func TestPBAOEHitsSelfExcluded(t *testing.T) {
	s, _ := m3Sim(t)
	p := m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	mob := m3Mob(s, "forest_boar", Vec3{4, 0, 0})
	if err := s.CastSkill(1, CastRequest{SkillID: "whirlwind", TargetID: mob.ID}); err != nil {
		t.Fatalf("cast: %v", err)
	}
	s.mu.Lock()
	pHP := p.HP
	mobHP := mob.HP
	s.mu.Unlock()
	if pHP != p.MaxHP {
		t.Fatalf("pbaoe self-hit: player hp %d != %d", pHP, p.MaxHP)
	}
	if mobHP >= mob.MaxHP {
		t.Fatalf("pbaoe missed the mob (hp=%d/%d)", mobHP, mob.MaxHP)
	}
}

func TestLevelUpCarriesXP(t *testing.T) {
	s, ts := m3Sim(t)
	p := m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	p.XP = 39 // one XP short of level 2 (need 40)
	// Give XP via a mob kill.
	m := m3Mob(s, "forest_boar", Vec3{2, 0, 0})
	if err := s.CastSkill(1, CastRequest{SkillID: "execute", TargetID: m.ID}); err != nil {
		t.Fatalf("cast: %v", err)
	}
	s.mu.Lock()
	level := p.Level
	xp := p.XP
	s.mu.Unlock()
	if level != 2 {
		t.Fatalf("level = %d, want 2", level)
	}
	if xp != 39+20-40 {
		t.Fatalf("carryover xp = %d", xp)
	}
	if ts.level != 2 {
		t.Fatalf("persisted level = %d, want 2", ts.level)
	}
	// MaxHP scales: 100 + 20*(level-1).
	if p.MaxHP != 120 {
		t.Fatalf("maxhp after level = %d, want 120", p.MaxHP)
	}
}

func TestMobThreatLeashAndReturn(t *testing.T) {
	s, _ := m3Sim(t)
	p := m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	// Spawn a boar just inside aggro range of the player.
	m := m3Mob(s, "forest_boar", Vec3{7, 0, 0}) // aggro radius 8
	s.tickOnce(time.Now())
	s.mu.Lock()
	state := m.state
	target := m.target
	s.mu.Unlock()
	if state != MobAggro {
		t.Fatalf("state = %s, want aggro", state)
	}
	if target != p.ID {
		t.Fatalf("target = %d, want player %d", target, p.ID)
	}
	// Player walks far away → mob leashes and returns to spawn.
	s.mu.Lock()
	m.Pos = Vec3{200, 0, 200}
	s.mu.Unlock()
	s.tickOnce(time.Now())
	s.mu.Lock()
	state = m.state
	s.mu.Unlock()
	if state != MobReturn {
		t.Fatalf("state = %s, want return after leash break", state)
	}
	// Let it walk home.
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.tickOnce(time.Now())
		s.mu.Lock()
		home := m.state == MobIdle && m.Pos.Distance(m.spawn) < 1.5
		s.mu.Unlock()
		if home {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("mob never returned home")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.HP != m.MaxHP {
		t.Fatalf("mob hp after return = %d, want full", m.HP)
	}
}

func TestMobRespawnsAfterDeath(t *testing.T) {
	s, _ := m3Sim(t)
	m := m3Mob(s, "forest_boar", Vec3{2, 0, 0})
	// Kill it directly via the sim's combat path.
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	s.mu.Lock()
	m.HP = 1
	s.mu.Unlock()
	if err := s.CastSkill(1, CastRequest{SkillID: "twin_slash", TargetID: m.ID}); err != nil {
		t.Fatalf("cast: %v", err)
	}
	s.mu.Lock()
	alive := m.alive
	s.mu.Unlock()
	if alive {
		t.Fatal("mob alive after kill")
	}
	// Fast-forward past the respawn window and tick.
	m.respawnAt = time.Now().Add(-time.Second)
	s.tickOnce(time.Now())
	s.mu.Lock()
	defer s.mu.Unlock()
	if !m.alive {
		t.Fatal("mob did not respawn")
	}
	if m.HP != m.MaxHP {
		t.Fatalf("respawned hp = %d, want %d", m.HP, m.MaxHP)
	}
}

func TestChatWorldAndMute(t *testing.T) {
	s, _ := m3Sim(t)
	m3Player(s, 1, "blade_dancer", Vec3{0, 0, 0})
	b := m3Player(s, 2, "spellweaver", Vec3{200, 0, 200}) // far away: say won't reach
	if err := s.SendChat(1, "world", "hello everyone"); err != nil {
		t.Fatalf("world chat: %v", err)
	}
	msgs := readChatMessages(t, b)
	if len(msgs) != 1 || msgs[0].Text != "hello everyone" || msgs[0].SenderName != "P" {
		t.Fatalf("world chat not relayed: %+v", msgs)
	}
	// Say channel: b is too far, so nothing arrives.
	if err := s.SendChat(1, "say", "nearby only"); err != nil {
		t.Fatalf("say chat: %v", err)
	}
	msgs = readChatMessages(t, b)
	if len(msgs) != 0 {
		t.Fatalf("say chat leaked across range: %+v", msgs)
	}
	// Mute a: sending is rejected.
	s.MuteCharacter(1, true)
	if err := s.SendChat(1, "world", "ignored"); err == nil {
		t.Fatal("muted player could send chat")
	}
}

func readChatMessages(t *testing.T, p *Player) []*aet.ChatMessage {
	t.Helper()
	var out []*aet.ChatMessage
	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case frame := <-p.Outbox:
			env := &aet.Envelope{}
			if proto.Unmarshal(frame, env) != nil {
				continue
			}
			if env.PayloadType != "aetheria.ChatMessage" {
				continue
			}
			cm := &aet.ChatMessage{}
			if proto.Unmarshal(env.Payload, cm) == nil {
				out = append(out, cm)
			}
		case <-deadline:
			return out
		}
	}
}

func TestLoadContentFromDisk(t *testing.T) {
	c, err := LoadContent("../../../shared/content")
	if err != nil {
		t.Fatalf("load content: %v", err)
	}
	if len(c.Skills) < 12 {
		t.Fatalf("skills = %d, want >= 12", len(c.Skills))
	}
	if len(c.Mobs) < 10 {
		t.Fatalf("mobs = %d, want >= 10", len(c.Mobs))
	}
	if _, ok := c.Skills["blade_strike"]; !ok {
		t.Fatal("missing blade_strike")
	}
	if _, ok := c.Skills["arcane_bolt"]; !ok {
		t.Fatal("missing arcane_bolt")
	}
	if _, ok := c.Mobs["ashmaw"]; !ok {
		t.Fatal("missing ashmaw")
	}
	// M5 seeds: the 15-quest Havenport chain and its NPCs must load.
	if len(c.Quests) != 15 {
		t.Fatalf("quests = %d, want 15", len(c.Quests))
	}
	for _, id := range []string{
		"q_welcome", "q_boar_pests", "q_hide_collector", "q_wolf_threat",
		"q_rat_plague", "q_thorn_clearing", "q_gear_up", "q_hound_hunt",
		"q_brute_force", "q_wraith_warden", "q_magma_stopper",
		"q_lava_crawler_clear", "q_ashmaw_fang", "q_field_report", "q_hero_welcome",
	} {
		if _, ok := c.Quests[id]; !ok {
			t.Fatalf("missing quest %s", id)
		}
	}
	// Chain integrity: every next_quest is a defined quest.
	for id, q := range c.Quests {
		if q.NextQuest == "" {
			continue
		}
		if _, ok := c.Quests[q.NextQuest]; !ok {
			t.Fatalf("quest %s next_quest %q undefined", id, q.NextQuest)
		}
		if q.GiverNPC == "" || q.TurninNPC == "" {
			t.Fatalf("quest %s missing giver/turnin npc", id)
		}
		if _, ok := c.NPCs[q.GiverNPC]; !ok {
			t.Fatalf("quest %s giver %q undefined", id, q.GiverNPC)
		}
		if _, ok := c.NPCs[q.TurninNPC]; !ok {
			t.Fatalf("quest %s turnin %q undefined", id, q.TurninNPC)
		}
	}
	if _, ok := c.NPCs["aldric_questgiver"]; !ok {
		t.Fatal("missing aldric_questgiver")
	}
	if _, ok := c.NPCs["vendor_maren"]; !ok {
		t.Fatal("missing vendor_maren")
	}
}
