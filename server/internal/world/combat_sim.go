// Combat + chat dispatch in the sim. All methods are called with s.mu held
// (from tickOnce) or acquire it themselves (external wire entry points).
package world

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// Combat validation errors (surfaced as reason strings to the client).
var (
	ErrSkillUnknown    = errors.New("unknown skill")
	ErrSkillWrongClass = errors.New("skill not usable by class")
	ErrNoTarget        = errors.New("no target")
	ErrOutOfRange      = errors.New("target out of range")
	ErrCooldown        = errors.New("skill on cooldown")
	ErrNoMP            = errors.New("not enough mp")
	ErrSafeZone        = errors.New("combat forbidden in safe zone")
	ErrDead            = errors.New("cannot act while dead")
)

// CastRequest is a validated skill cast from the wire layer.
type CastRequest struct {
	SkillID  string
	TargetID uint64
	AimPos   *Vec3
}

// CastSkill validates and resolves a skill cast for a player. Returns an error
// reason (one of the Err* above) when the cast is rejected, or nil when the
// skill resolved (damage applied, events emitted, MP/cooldown consumed).
// Safe zones reject all combat skills (brief §6).
func (s *Sim) CastSkill(charID int64, req CastRequest) error {
	if req.SkillID == "" {
		return ErrSkillUnknown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.castSkillLocked(s.players[charID], req, time.Now())
}

// SetAutoAttack sets or clears a player's auto-attack target.
func (s *Sim) SetAutoAttack(charID int64, targetID uint64, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	if !active {
		p.autoAttack = 0
		return nil
	}
	p.autoAttack = targetID
	return nil
}

// RespawnPlayer resurrects a dead character at its zone shrine, restoring
// full HP/MP. Used by the wire layer on RespawnRequest.
func (s *Sim) RespawnPlayer(charID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	if p.HP > 0 {
		return errors.New("not dead")
	}
	s.respawnLocked(p)
	return nil
}

// SendChat relays a chat message per channel rules (say = nearby, world =
// zone-wide). Returns an error when rejected (muted, bad channel, empty).
func (s *Sim) SendChat(charID int64, channel, text string) error {
	text = trimText(text)
	if text == "" {
		return errors.New("empty message")
	}
	if channel != "say" && channel != "world" {
		return errors.New("unknown channel")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	if s.muted[p.CharacterID] {
		return errors.New("muted")
	}
	msg := &aet.ChatMessage{
		Channel:      channel,
		Text:         text,
		SenderId:     p.ID,
		SenderName:   p.Name,
		SentAtUnixMs: time.Now().UnixMilli(),
	}
	if channel == "world" {
		for _, other := range s.players {
			if other.Zone == p.Zone {
				s.sendEvent(other, msg)
			}
		}
	} else {
		// say: everyone within 30 m.
		for _, other := range s.players {
			if other.Zone == p.Zone && p.Pos.Distance(other.Pos) <= 30 {
				s.sendEvent(other, msg)
			}
		}
	}
	return nil
}

// MuteCharacter mutes/unmutes a character by id (chat mute, M3).
func (s *Sim) MuteCharacter(charID int64, mute bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mute {
		s.muted[charID] = true
	} else {
		delete(s.muted, charID)
	}
}

// ---- internal (callers hold s.mu) ----

// castSkillLocked resolves a cast; caller holds s.mu and p may be nil.
func (s *Sim) castSkillLocked(p *Player, req CastRequest, now time.Time) error {
	if p == nil {
		return errors.New("character not in world")
	}
	if p.HP <= 0 {
		return ErrDead
	}
	def := s.skills[req.SkillID]
	if def == nil {
		return ErrSkillUnknown
	}
	if def.Class != "mob" && def.Class != p.Class {
		return ErrSkillWrongClass
	}
	if z := s.zones[p.Zone]; z != nil && z.Safe {
		return ErrSafeZone
	}
	if cd := p.cooldowns[req.SkillID]; cd > now.UnixMilli() {
		return ErrCooldown
	}
	if p.MP < def.CostMP {
		return ErrNoMP
	}
	// Range check (tabtarget/auto: distance to target; aimed: distance to aim
	// point; pbaoe/self: no range requirement).
	switch def.Kind {
	case SkillTabTarget, SkillAuto:
		e := s.entityByID(req.TargetID)
		if e == nil {
			return ErrNoTarget
		}
		if e.Zone != p.Zone || p.Pos.Distance(e.Pos) > def.Range {
			return ErrOutOfRange
		}
	case SkillAimed:
		if req.AimPos == nil {
			return errors.New("aim position required")
		}
		if p.Pos.Distance(*req.AimPos) > def.Range {
			return ErrOutOfRange
		}
	}
	if def.CostMP > 0 {
		p.MP -= def.CostMP
		p.dirtySelf = true
	}
	if def.CooldownMS > 0 {
		if p.cooldowns == nil {
			p.cooldowns = map[string]int64{}
		}
		p.cooldowns[req.SkillID] = now.UnixMilli() + def.CooldownMS
	}
	if err := s.resolveHit(p, def, req, now); err != nil {
		return err
	}
	return nil
}

// resolveHit applies the skill's effect based on its kind.
func (s *Sim) resolveHit(p *Player, def *SkillDef, req CastRequest, now time.Time) error {
	switch def.Kind {
	case SkillSelf:
		// Shield buff: absorbs incoming damage until the expiry.
		if p.Shield < def.Power {
			p.Shield = def.Power
		}
		p.shieldExpiry = now.Add(15 * time.Second)
		p.dirtySelf = true
		s.sendCombat(p.ID, p.ID, def.ID, "shield", def.Power, fmt.Sprintf("%s gains a shield", p.Name))
		return nil
	case SkillAimed:
		// Ground AoE: hits every entity in the template radius around the aim.
		if req.AimPos == nil {
			return errors.New("aim position required")
		}
		return s.applyAreaDamage(p, def, *req.AimPos)
	case SkillPBAOE:
		return s.applyAreaDamage(p, def, p.Pos)
	default:
		// tabtarget / auto: single target.
		if req.TargetID == 0 {
			return ErrNoTarget
		}
		return s.applySingleTarget(p, def, req.TargetID)
	}
}

// applySingleTarget deals damage to one entity.
func (s *Sim) applySingleTarget(p *Player, def *SkillDef, targetID uint64) error {
	e := s.entityByID(targetID)
	if e == nil {
		return errors.New("target not found")
	}
	if e.Type == TypeMob {
		m := s.mobs[targetID]
		if m == nil || !m.alive {
			return errors.New("target not found")
		}
		s.damageEntity(p, &m.Entity, def, time.Now())
	} else if e.Type == TypePlayer {
		tp := s.byEntity[targetID]
		if tp == nil {
			return errors.New("target not found")
		}
		s.damageEntity(p, &tp.Entity, def, time.Now())
	} else {
		return errors.New("target not damageable")
	}
	return nil
}

// applyAreaDamage hits every damageable entity within def.Radius of center,
// excluding the caster (pbaoe never self-hits).
func (s *Sim) applyAreaDamage(p *Player, def *SkillDef, center Vec3) error {
	near := s.grid.Nearby(center, p.ID)
	now := time.Now()
	for _, e := range near {
		if e.Zone != p.Zone {
			continue
		}
		if center.Distance(e.Pos) > def.Radius {
			continue
		}
		if e.Type == TypeMob {
			if m := s.mobs[e.ID]; m != nil && m.alive {
				s.damageEntity(p, &m.Entity, def, now)
			}
		} else if e.Type == TypePlayer {
			if tp := s.byEntity[e.ID]; tp != nil {
				s.damageEntity(p, &tp.Entity, def, now)
			}
		}
	}
	return nil
}

// entityByID returns the entity for an id, or nil.
func (s *Sim) entityByID(id uint64) *Entity {
	if p, ok := s.byEntity[id]; ok {
		return &p.Entity
	}
	if m, ok := s.mobs[id]; ok {
		return &m.Entity
	}
	return nil
}

// damageEntity applies damage from attacker to victim and manages death/XP.
func (s *Sim) damageEntity(attacker *Player, victim *Entity, def *SkillDef, now time.Time) {
	if victim.Type == TypeMob {
		m := s.mobs[victim.ID]
		if m == nil || !m.alive {
			return
		}
		res := RollDamage(def.Power+attacker.Attack(), attacker.Level, m.Level)
		m.HP -= res.Damage
		m.threat[attacker.ID] += res.Damage
		// Aggro: target the threat leader.
		if m.state == MobIdle {
			m.state = MobAggro
		}
		s.sendCombat(attacker.ID, m.ID, def.ID, "hit", res.Damage, fmt.Sprintf("%s hits %s for %d", attacker.Name, m.Name, res.Damage))
		if m.HP <= 0 {
			m.HP = 0
			s.killMob(m, attacker, now)
		}
		return
	}
	if victim.Type == TypePlayer {
		tp := s.byEntity[victim.ID]
		if tp == nil {
			return
		}
		res := RollDamage(def.Power, attacker.Level, tp.Level)
		// Equipment defense mitigates incoming damage (M4).
		if d := tp.Defense(); d > 0 {
			res.Damage -= d
			if res.Damage < 1 {
				res.Damage = 1
			}
		}
		if tp.Shield > 0 {
			res.Damage, tp.Shield = ApplyShield(res.Damage, tp.Shield)
		}
		tp.HP -= res.Damage
		tp.dirtySelf = true
		if tp.autoAttack == 0 {
			tp.autoAttack = attacker.ID
		}
		s.sendCombat(attacker.ID, tp.ID, def.ID, "hit", res.Damage, fmt.Sprintf("%s hits %s for %d", attacker.Name, tp.Name, res.Damage))
		if tp.HP <= 0 {
			tp.HP = 0
			s.killPlayer(tp, now)
		}
	}
}

// killMob awards XP and removes the mob (respawning later).
func (s *Sim) killMob(m *Mob, killer *Player, now time.Time) {
	m.alive = false
	m.HP = 0
	m.state = MobIdle
	m.threat = map[uint64]int64{}
	m.respawnAt = now.Add(30 * time.Second)
	s.grid.Remove(&m.Entity)
	s.sendCombat(killer.ID, m.ID, "kill", "kill", 0, fmt.Sprintf("%s kills %s", killer.Name, m.Name))
	// XP to the killer (and anyone who hit the mob in the last 10 s — simple:
	// full XP to killer for M3).
	s.grantXP(killer, m)
	// Gold + loot rolls (M4): flat gold credits the killer via the ledger;
	// drop tables for this mob def spawn ground drops at its position.
	if def := s.mobDefs[m.DefID]; def != nil && def.GoldReward > 0 {
		s.creditGold(killer, def.GoldReward, "mob_kill")
	}
	s.rollLoot(m.DefID, m.Pos, m.Zone)
}

// grantXP awards a mob's XP to a player and handles level-ups.
func (s *Sim) grantXP(p *Player, m *Mob) {
	def := s.mobDefs[m.DefID]
	if def == nil {
		return
	}
	p.XP += def.XPReward
	s.sendCombat(p.ID, m.ID, "xp", "xp", def.XPReward, fmt.Sprintf("%s gains %d XP", p.Name, def.XPReward))
	level, xp, leveled := XPToLevel(p.Level, p.XP)
	p.XP = xp
	if leveled {
		p.Level = level
		p.MaxHP = 100 + 20*int64(level-1)
		p.HP = p.MaxHP
		p.MaxMP = 50 + 10*int64(level-1)
		p.MP = p.MaxMP
		p.dirtySelf = true
		s.sendCombat(p.ID, p.ID, "level_up", "level_up", 0, fmt.Sprintf("%s reaches level %d!", p.Name, level))
	}
	s.persistChar(p)
}

// killPlayer sends a death event and despawns the player until respawn.
func (s *Sim) killPlayer(p *Player, now time.Time) {
	p.HP = 0
	p.dirtySelf = true
	p.autoAttack = 0
	s.sendCombat(p.ID, p.ID, "death", "death", 0, fmt.Sprintf("%s has died", p.Name))
	// Despawn from AOI; respawn will re-insert.
	s.grid.Remove(&p.Entity)
	s.emitSnapshot(p)
}

// respawnLocked restores a dead player at the zone shrine.
func (s *Sim) respawnLocked(p *Player) {
	p.HP = p.MaxHP
	p.MP = p.MaxMP
	p.Shield = 0
	p.autoAttack = 0
	p.target = 0
	if shrine, ok := s.shrines[p.Zone]; ok {
		p.Pos = shrine
		if z := s.zoneFor(p.Pos); z != nil {
			p.Zone = z.ID
		}
	}
	p.dirtySelf = true
	p.known = make(map[uint64]*aet.EntityState)
	s.grid.Insert(&p.Entity)
	s.sendCombat(p.ID, p.ID, "respawn", "respawn", 0, fmt.Sprintf("%s respawns", p.Name))
	s.persistChar(p)
}

// processAutoAttack performs a player's auto-attack on their target each tick.
func (s *Sim) processAutoAttack(p *Player, now time.Time) {
	if p.autoAttack == 0 || p.HP <= 0 {
		return
	}
	def := s.skills[autoSkillFor(p.Class)]
	if def == nil {
		return
	}
	e := s.entityByID(p.autoAttack)
	if e == nil {
		p.autoAttack = 0
		return
	}
	if e.Zone != p.Zone || p.Pos.Distance(e.Pos) > def.Range {
		return
	}
	if cd := p.cooldowns[autoSkillFor(p.Class)]; cd > now.UnixMilli() {
		return
	}
	// Auto attacks are free, no MP cost, short cooldown.
	p.cooldowns[autoSkillFor(p.Class)] = now.UnixMilli() + 1000
	req := CastRequest{SkillID: autoSkillFor(p.Class), TargetID: p.autoAttack}
	s.resolveHit(p, def, req, now)
}

// autoSkillFor returns the auto-attack skill id for a class.
func autoSkillFor(class string) string {
	if class == "spellweaver" {
		return "arcane_bolt"
	}
	return "blade_strike"
}

// tickMob advances one mob's AI.
func (s *Sim) tickMob(m *Mob, now time.Time) {
	def := s.mobDefs[m.DefID]
	if def == nil {
		return
	}
	if !m.alive {
		if now.After(m.respawnAt) {
			s.respawnMob(m)
		}
		return
	}
	switch m.state {
	case MobIdle:
		// Check for nearby players to aggro.
		near := s.grid.Nearby(m.Pos, m.ID)
		for _, e := range near {
			if e.Type == TypePlayer && e.Zone == m.Zone {
				if p := s.byEntity[e.ID]; p != nil && p.HP > 0 && m.Pos.Distance(p.Pos) <= def.AggroRadius {
					m.state = MobAggro
					m.target = p.ID
					m.threat[p.ID] = 1 // seed minimal threat
					break
				}
			}
		}
	case MobAggro:
		target := s.byEntity[m.target]
		if target == nil || target.HP <= 0 {
			m.target = s.threatLeader(m)
			target = s.byEntity[m.target]
		}
		if target == nil || target.HP <= 0 || target.Zone != m.Zone {
			m.state = MobReturn
			return
		}
		// Leash: return if too far from spawn.
		if m.Pos.Distance(m.spawn) > def.LeashRadius {
			m.state = MobReturn
			return
		}
		s.followAndAttack(m, def, target)
	case MobReturn:
		// Walk back to spawn and reset once home.
		to := m.spawn.Sub(m.Pos)
		if to.Len() < 1.5 {
			m.Pos = m.spawn
			m.HP = m.MaxHP
			m.state = MobIdle
			m.target = 0
			m.threat = map[uint64]int64{}
			m.Moving = false
			return
		}
		step := m.moveSpeed() * s.tick.Seconds()
		m.Pos = m.Pos.Add(to.Normalize().Mul(step))
		m.Moving = true
		s.grid.Refresh(&m.Entity)
	}
}

// threatLeader returns the entity id with the highest threat.
func (s *Sim) threatLeader(m *Mob) uint64 {
	var bestID uint64
	var best int64
	for id, t := range m.threat {
		if t > best {
			best = t
			bestID = id
		}
	}
	return bestID
}

// followAndAttack moves the mob toward its target and uses skills in range.
func (s *Sim) followAndAttack(m *Mob, def *MobDef, target *Player) {
	dist := m.Pos.Distance(target.Pos)
	// Move into melee range (2 m) if not already there.
	if dist > 2 {
		step := m.moveSpeed() * s.tick.Seconds()
		dir := target.Pos.Sub(m.Pos).Normalize()
		m.Pos = m.Pos.Add(dir.Mul(step))
		m.RotY = atan2(dir.Z, dir.X)
		m.Moving = true
		s.grid.Refresh(&m.Entity)
		return
	}
	m.Moving = false
	// Attack with the first skill off cooldown.
	for _, sid := range def.Skills {
		sk := s.skills[sid]
		if sk == nil {
			continue
		}
		if cd := m.skillCooldowns[sid]; cd > time.Now().UnixMilli() {
			continue
		}
		m.skillCooldowns[sid] = time.Now().UnixMilli() + sk.CooldownMS
		s.mobHit(m, def, sk, target)
		return
	}
}

// mobHit applies a mob's skill damage to a player target.
func (s *Sim) mobHit(m *Mob, def *MobDef, sk *SkillDef, target *Player) {
	res := RollDamage(sk.Power, int32(def.Level), target.Level)
	if target.Shield > 0 {
		res.Damage, target.Shield = ApplyShield(res.Damage, target.Shield)
	}
	target.HP -= res.Damage
	target.dirtySelf = true
	if target.autoAttack == 0 {
		target.autoAttack = m.ID
	}
	s.sendCombat(m.ID, target.ID, sk.ID, "hit", res.Damage, fmt.Sprintf("%s hits %s for %d", m.Name, target.Name, res.Damage))
	if target.HP <= 0 {
		target.HP = 0
		s.killPlayer(target, time.Now())
	}
}

func (m *Mob) moveSpeed() float64 { return 5.0 }

// sendCombat pushes a CombatEvent envelope to a player's outbox.
func (s *Sim) sendCombat(src, tgt uint64, skillID, eventType string, amount int64, msg string) {
	ev := &aet.CombatEvent{
		EventType: eventType,
		SourceId:  src,
		TargetId:  tgt,
		SkillId:   skillID,
		Amount:    amount,
		Message:   msg,
	}
	// Notify the attacker if a player, plus the target if a player.
	if p := s.byEntity[src]; p != nil {
		s.sendEvent(p, ev)
	}
	if p := s.byEntity[tgt]; p != nil {
		s.sendEvent(p, ev)
	}
}

// sendEvent pushes an envelope (combat/chat) to a player's outbox.
func (s *Sim) sendEvent(p *Player, msg proto.Message) {
	if p == nil || p.Outbox == nil {
		return
	}
	frame, err := proto.Marshal(&aet.Envelope{
		Kind:        aet.Envelope_KIND_EVENT,
		PayloadType: eventTypeFor(msg),
		Payload:     mustMarshal(msg),
	})
	if err != nil {
		s.log("warn: marshal event: %v", err)
		return
	}
	select {
	case p.Outbox <- frame:
	default:
	}
}

// eventTypeFor returns the wire payload type for a known message.
func eventTypeFor(m proto.Message) string {
	switch m.(type) {
	case *aet.CombatEvent:
		return "aetheria.CombatEvent"
	case *aet.ChatMessage:
		return "aetheria.ChatMessage"
	case *aet.LootEvent:
		return "aetheria.LootEvent"
	}
	return "aetheria.UnknownEvent"
}

// respawnMob re-inserts a dead mob at its spawn position.
func (s *Sim) respawnMob(m *Mob) {
	m.HP = m.MaxHP
	m.alive = true
	m.state = MobIdle
	m.threat = map[uint64]int64{}
	m.target = 0
	m.Pos = m.spawn
	m.skillCooldowns = map[string]int64{}
	s.grid.Insert(&m.Entity)
}

// persistChar writes level/xp/hp/mp to the DB via SaveChar (best effort).
func (s *Sim) persistChar(p *Player) {
	if s.SaveChar == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.SaveChar(ctx, p.CharacterID, p.Level, p.XP, p.HP, p.MP); err != nil {
		s.log("warn: save char char=%d: %v", p.CharacterID, err)
	}
}

func trimText(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			continue
		}
		out = append(out, []byte(string(r))...)
	}
	return string(out)
}
