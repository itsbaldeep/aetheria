// Quests (brief §141, M5): a linear breadcrumb chain of kill/collect/talk
// objectives with XP/gold/item rewards. Quest state is per-player, persisted to
// character_quests (state + objective progress). Gold rewards flow through
// creditGold (audited ledger, reason "quest_reward"); XP through the standard
// level-up path. All entry points are thread-safe (acquire s.mu).
package world

import (
	"context"
	"errors"
	"fmt"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// talkRadius is how close a player must be to an NPC to talk to it.
const talkRadius = 4.0

// QuestProgress is a player's per-quest state (M5).
type QuestProgress struct {
	QuestID string // quest def id
	State   string // active|complete|abandoned
	Counts  []int32
}

// QuestState is a snapshot of a player's progress on one quest.
type QuestState struct {
	QuestID    string
	Name       string
	State      string
	TurninNPC  string
	Objectives []QuestObjectiveState
}

// QuestObjectiveState is one objective's progress (for wire/events).
type QuestObjectiveState struct {
	Type       string
	Target     string
	TargetName string
	Current    int32
	Required   int32
}

// ErrQuest errors (surfaced as reason strings to the client).
var (
	ErrQuestUnknown    = errors.New("unknown quest")
	ErrQuestNotAvail   = errors.New("quest not available")
	ErrQuestActive     = errors.New("quest already active")
	ErrQuestInactive   = errors.New("quest not active")
	ErrQuestIncomplete = errors.New("quest objectives incomplete")
	ErrQuestComplete   = errors.New("quest already complete")
	ErrNoNPC           = errors.New("no npc nearby")
	ErrLevelTooLow     = errors.New("level too low")
)

// spawnNPC places a static NPC entity into the world grid at its def position.
// NPCs never move and are never combat targets; they exist so players can walk
// up and talk (NpcInteract within talkRadius).
func (s *Sim) spawnNPC(npc *NPC) {
	s.nextID++
	e := &Entity{
		ID:    s.nextID,
		Type:  TypeNPC,
		Name:  npc.Name,
		Zone:  npc.ZoneID,
		Pos:   npc.Pos,
		HP:    1,
		MaxHP: 1,
		Level: 1,
		RefID: npc.ID,
	}
	s.npcEntities[npc.ID] = e
	s.grid.Insert(e)
}

// NPCs returns the static NPC entities in the world (snapshot copies).
func (s *Sim) NPCs() []*Entity {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Entity, 0, len(s.npcEntities))
	for _, e := range s.npcEntities {
		out = append(out, e)
	}
	return out
}

// NpcDialog is the result of talking to an NPC: the NPC's flavor text plus the
// quests it offers (giver) and the active quests ready to turn in here.
type NpcDialog struct {
	NPCID     string
	NPCName   string
	Dialog    string
	Available []string // quest ids this NPC offers
	Turnin    []string // active quest ids complete and turn-in-able here
}

// NpcInteract talks to an NPC: validates proximity, advances talk objectives,
// and returns the dialog with available/turn-in quests.
func (s *Sim) NpcInteract(charID int64, npcID string) (*NpcDialog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil, errors.New("character not in world")
	}
	e, ok := s.npcEntities[npcID]
	if !ok {
		return nil, errors.New("unknown npc")
	}
	if e.Zone != p.Zone || p.Pos.Distance(e.Pos) > talkRadius {
		return nil, ErrNoNPC
	}
	npc := s.vendors[npcID]
	dialog := &NpcDialog{NPCID: npcID}
	if npc != nil {
		dialog.NPCName = npc.Name
		dialog.Dialog = npc.Dialog
	}
	// Advance talk objectives targeting this NPC.
	for _, qp := range p.Quests {
		if qp.State != "active" {
			continue
		}
		changed := false
		for i, obj := range s.questDefs[qp.QuestID].Objectives {
			if obj.Type == ObjectiveTalk && obj.Target == npcID && qp.Counts[i] < obj.Count {
				qp.Counts[i] = obj.Count
				changed = true
			}
		}
		if changed {
			s.pushQuestEventLocked(p, qp)
		}
	}
	// Available = offered here, level ok, not active/complete, chain unlocked.
	for qid, def := range s.questDefs {
		if def.GiverNPC != npcID {
			continue
		}
		if s.questAvailableLocked(p, def) {
			dialog.Available = append(dialog.Available, qid)
		}
	}
	// Turn-in = active, complete objectives, turn-in at this NPC.
	for qid, qp := range p.Quests {
		if qp.State != "active" {
			continue
		}
		def := s.questDefs[qid]
		if def == nil || def.TurninNPC != npcID {
			continue
		}
		if s.questCompleteLocked(qp, def) {
			dialog.Turnin = append(dialog.Turnin, qid)
		}
	}
	return dialog, nil
}

// AcceptQuest starts a quest. Fails when the quest is unknown, the character is
// too low level, the chain isn't unlocked, or the quest is already active.
func (s *Sim) AcceptQuest(charID int64, questID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	def := s.questDefs[questID]
	if def == nil {
		return ErrQuestUnknown
	}
	if !s.questAvailableLocked(p, def) {
		return ErrQuestNotAvail
	}
	if p.Quests == nil {
		p.Quests = map[string]*QuestProgress{}
	}
	qp := &QuestProgress{QuestID: questID, State: "active", Counts: make([]int32, len(def.Objectives))}
	p.Quests[questID] = qp
	s.pushQuestEventLocked(p, qp)
	s.persistQuestsLocked(p)
	return nil
}

// AbandonQuest drops an active quest (progress cleared). It can be accepted
// again; the chain stays locked until the prerequisite is re-completed.
func (s *Sim) AbandonQuest(charID int64, questID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	qp := p.Quests[questID]
	if qp == nil || qp.State != "active" {
		return ErrQuestInactive
	}
	def := s.questDefs[questID]
	qp.State = "abandoned"
	qp.Counts = make([]int32, len(def.Objectives))
	s.pushQuestEventLocked(p, qp)
	s.persistQuestsLocked(p)
	return nil
}

// TurnInQuest completes an active quest when every objective is met, granting
// XP/gold (ledger "quest_reward")/items and unlocking the next chain quest.
func (s *Sim) TurnInQuest(charID int64, questID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	qp := p.Quests[questID]
	if qp == nil || qp.State != "active" {
		return ErrQuestInactive
	}
	def := s.questDefs[questID]
	if !s.questCompleteLocked(qp, def) {
		return ErrQuestIncomplete
	}
	if e, ok := s.npcEntities[def.TurninNPC]; !ok || e.Zone != p.Zone || p.Pos.Distance(e.Pos) > talkRadius {
		return ErrNoNPC
	}
	qp.State = "complete"
	// Rewards.
	if def.Rewards.XP > 0 {
		s.grantQuestXP(p, def.Rewards.XP)
	}
	if def.Rewards.Gold > 0 {
		s.creditGold(p, def.Rewards.Gold, "quest_reward")
	}
	for _, ri := range def.Rewards.Items {
		it := &Item{DefID: ri.DefID, Qty: ri.Qty, Stats: s.itemStats(ri.DefID)}
		if !s.addItem(p, it) {
			s.log("warn: quest %s reward item %s dropped (inventory full)", questID, ri.DefID)
		}
	}
	s.pushQuestEventLocked(p, qp)
	s.persistQuestsLocked(p)
	return nil
}

// QuestStatus returns the player's state for every quest def.
func (s *Sim) QuestStatus(charID int64) []QuestState {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil
	}
	out := make([]QuestState, 0, len(s.questDefs))
	for qid, def := range s.questDefs {
		qp := p.Quests[qid]
		st := QuestState{
			QuestID:   qid,
			Name:      def.Name,
			TurninNPC: def.TurninNPC,
			State:     "available",
		}
		if qp != nil {
			st.State = qp.State
			st.objectivesOf(qp, def)
		}
		out = append(out, st)
	}
	return out
}

// ---- internal (callers hold s.mu) ----

// questAvailableLocked reports whether a player may accept a quest: level met,
// not already active/complete, and the chain (previous quest via NextQuest) is
// complete. Chain heads (no quest points to them) are always available once
// level met.
func (s *Sim) questAvailableLocked(p *Player, def *QuestDef) bool {
	if p.Level < def.MinLevel {
		return false
	}
	if qp := p.Quests[def.ID]; qp != nil {
		if qp.State == "active" || qp.State == "complete" {
			return false
		}
	}
	// Chain: the quest pointing to this one must be complete.
	for _, other := range s.questDefs {
		if other.NextQuest == def.ID {
			qp := p.Quests[other.ID]
			return qp != nil && qp.State == "complete"
		}
	}
	return true // chain head
}

// questCompleteLocked reports whether every objective count is met.
func (s *Sim) questCompleteLocked(qp *QuestProgress, def *QuestDef) bool {
	for i, obj := range def.Objectives {
		if i >= len(qp.Counts) || qp.Counts[i] < obj.Count {
			return false
		}
	}
	return true
}

// advanceQuestLocked bumps an objective for the player and pushes a QuestEvent
// when progress changed. Used by kill/collect/talk hooks.
func (s *Sim) advanceQuestLocked(p *Player, wantType, wantTarget string, amount int32) {
	for _, qp := range p.Quests {
		if qp.State != "active" {
			continue
		}
		def := s.questDefs[qp.QuestID]
		if def == nil {
			continue
		}
		changed := false
		for i, obj := range def.Objectives {
			if obj.Type == wantType && obj.Target == wantTarget && qp.Counts[i] < obj.Count {
				qp.Counts[i] += amount
				if qp.Counts[i] > obj.Count {
					qp.Counts[i] = obj.Count
				}
				changed = true
			}
		}
		if changed {
			s.pushQuestEventLocked(p, qp)
		}
	}
}

// killHook advances kill objectives; called from killMob.
func (s *Sim) killHook(p *Player, mobDefID string) {
	if p == nil {
		return
	}
	s.advanceQuestLocked(p, ObjectiveKill, mobDefID, 1)
}

// collectHook advances collect objectives; called when an item enters a
// player's inventory (pickup / buy).
func (s *Sim) collectHook(p *Player, itemDefID string, qty int32) {
	if p == nil {
		return
	}
	s.advanceQuestLocked(p, ObjectiveCollect, itemDefID, qty)
}

// grantQuestXP adds quest XP through the standard level-up path.
func (s *Sim) grantQuestXP(p *Player, xp int64) {
	p.XP += xp
	level, remaining, leveled := XPToLevel(p.Level, p.XP)
	p.XP = remaining
	if leveled {
		p.Level = level
		p.MaxHP = 100 + 20*int64(level-1)
		p.HP = p.MaxHP
		p.MaxMP = 50 + 10*int64(level-1)
		p.MP = p.MaxMP
		p.dirtySelf = true
		s.sendCombat(p.ID, p.ID, "quest_reward", "level_up", 0, fmt.Sprintf("%s reaches level %d!", p.Name, level))
	}
	s.persistChar(p)
}

// pushQuestEventLocked sends a QuestEvent frame for a quest state change.
func (s *Sim) pushQuestEventLocked(p *Player, qp *QuestProgress) {
	def := s.questDefs[qp.QuestID]
	if def == nil {
		return
	}
	ev := &aet.QuestEvent{
		Ok:         true,
		QuestId:    qp.QuestID,
		Name:       def.Name,
		State:      qp.State,
		TurninNpc:  def.TurninNPC,
		Objectives: make([]*aet.QuestObjectiveState, len(def.Objectives)),
	}
	for i, obj := range def.Objectives {
		cur := int32(0)
		if i < len(qp.Counts) {
			cur = qp.Counts[i]
		}
		ev.Objectives[i] = &aet.QuestObjectiveState{
			Type:       obj.Type,
			Target:     obj.Target,
			TargetName: s.questTargetNameLocked(obj),
			Current:    cur,
			Required:   obj.Count,
		}
	}
	s.sendEvent(p, ev)
}

// questTargetNameLocked resolves an objective's display name (mob/item/npc).
func (s *Sim) questTargetNameLocked(obj QuestObjective) string {
	switch obj.Type {
	case ObjectiveKill:
		if d := s.mobDefs[obj.Target]; d != nil {
			return d.Name
		}
	case ObjectiveCollect:
		if d := s.itemDefs[obj.Target]; d != nil {
			return d.Name
		}
	case ObjectiveTalk:
		if e := s.npcEntities[obj.Target]; e != nil {
			return e.Name
		}
	}
	return obj.Target
}

// persistQuestsLocked flushes quest state for one player via SaveQuests (M5).
func (s *Sim) persistQuestsLocked(p *Player) {
	if s.SaveQuests == nil {
		return
	}
	ctx, cancel := contextWithTimeout(3 * time.Second)
	defer cancel()
	if err := s.SaveQuests(ctx, p.CharacterID, p.Quests); err != nil {
		s.log("warn: save quests char=%d: %v", p.CharacterID, err)
	}
}

// PersistQuests serializes a player's quest state for the DB mirror.
func (s *Sim) PersistQuests(charID int64) []QuestProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil
	}
	out := make([]QuestProgress, 0, len(p.Quests))
	for _, qp := range p.Quests {
		c := *qp
		c.Counts = append([]int32(nil), qp.Counts...)
		out = append(out, c)
	}
	return out
}

// RestoreQuests places DB-loaded quest progress back onto a player (M5).
func (s *Sim) RestoreQuests(charID int64, quests []QuestProgress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	if p.Quests == nil {
		p.Quests = map[string]*QuestProgress{}
	}
	for i := range quests {
		qp := quests[i]
		qp.Counts = append([]int32(nil), qp.Counts...)
		p.Quests[qp.QuestID] = &qp
	}
	return nil
}

// objectivesOf copies objective progress into a QuestState (QuestStatus).
func (st *QuestState) objectivesOf(qp *QuestProgress, def *QuestDef) {
	st.Objectives = make([]QuestObjectiveState, len(def.Objectives))
	for i, obj := range def.Objectives {
		cur := int32(0)
		if i < len(qp.Counts) {
			cur = qp.Counts[i]
		}
		st.Objectives[i] = QuestObjectiveState{
			Type:     obj.Type,
			Target:   obj.Target,
			Current:  cur,
			Required: obj.Count,
		}
	}
}
