// The world simulation: owns all entities, runs the 20 Hz tick, applies
// validated movement intents, and streams AOI snapshots. Thread model:
// one simulation goroutine owns all state; external entry points lock mu;
// snapshots are marshalled and pushed into each player's Outbox channel,
// which the wire layer drains and writes to the socket.
package world

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// ErrBadMove is returned for movement intents that violate validation.
var ErrBadMove = errors.New("invalid movement intent")

// MoveIntent is a validated movement request. Zero Target + zero Direction
// means "stop".
type MoveIntent struct {
	Target    *Vec3
	Direction Vec3
	Speed     float64
	RotY      float64
}

// Zone is a playable area (brief §6). Safe zones forbid combat (M3).
type Zone struct {
	ID    string
	Name  string
	Safe  bool
	SizeX float64
	SizeZ float64
}

// Sim is the world simulation.
type Sim struct {
	mu sync.Mutex

	nextID uint64
	zones  map[string]*Zone
	grid   *Grid
	// players by character id.
	players map[int64]*Player
	// byEntity for O(1) lookups on disconnect.
	byEntity map[uint64]*Player

	// SavePos persists a character's position (called periodically + on leave).
	SavePos func(ctx context.Context, charID int64, pos Vec3) error

	logf func(format string, args ...any)
	tick time.Duration

	// outboxBuffer is the per-player outbox channel capacity.
	outboxBuffer int
}

// Options configures the simulation.
type Options struct {
	Zones        []*Zone
	Logf         func(format string, args ...any)
	Tick         time.Duration
	SavePos      func(ctx context.Context, charID int64, pos Vec3) error
	OutboxBuffer int
}

// New creates a world simulation. Caller must call Run in its own goroutine.
func New(opts Options) *Sim {
	if opts.Tick == 0 {
		opts.Tick = 50 * time.Millisecond
	}
	if opts.Logf == nil {
		opts.Logf = log.Printf
	}
	if opts.OutboxBuffer == 0 {
		opts.OutboxBuffer = 64
	}
	s := &Sim{
		zones:        make(map[string]*Zone),
		grid:         NewGrid(),
		players:      make(map[int64]*Player),
		byEntity:     make(map[uint64]*Player),
		SavePos:      opts.SavePos,
		logf:         opts.Logf,
		tick:         opts.Tick,
		outboxBuffer: opts.OutboxBuffer,
	}
	for _, z := range opts.Zones {
		s.zones[z.ID] = z
	}
	return s
}

// Run drives the tick loop until ctx is cancelled.
func (s *Sim) Run(ctx context.Context) {
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			s.tickOnce(now)
		}
	}
}

// Logf overrides the logger (for tests / silencing).
func (s *Sim) Logf(f func(format string, args ...any)) { s.logf = f }

func (s *Sim) log(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// NewPlayerOutbox builds a fresh outbox channel sized to the sim's capacity.
func (s *Sim) NewPlayerOutbox() chan []byte { return make(chan []byte, s.outboxBuffer) }

// Spawn registers a player at its position and returns its entity id. Called
// from the wire layer after EnterWorld validation.
func (s *Sim) Spawn(p *Player) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.players[p.CharacterID]; ok {
		return errors.New("character already in world")
	}
	if _, ok := s.zones[p.Zone]; !ok {
		return fmt.Errorf("unknown zone %q", p.Zone)
	}
	s.nextID++
	p.ID = s.nextID
	p.known = make(map[uint64]*aet.EntityState)
	p.dirtySelf = true
	s.players[p.CharacterID] = p
	s.byEntity[p.ID] = p
	s.grid.Insert(&p.Entity)
	return nil
}

// Despawn removes a player from the world. Called on disconnect.
func (s *Sim) Despawn(charID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return
	}
	s.grid.Remove(&p.Entity)
	delete(s.players, charID)
	delete(s.byEntity, p.ID)
}

// Player returns the player for a character id (nil if not in world).
func (s *Sim) Player(charID int64) *Player {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.players[charID]
}

// PlayerPos returns a copy of a player's current position (nil if absent).
// Thread-safe: safe to call from tests, bots, or the health endpoint while
// the sim runs.
func (s *Sim) PlayerPos(charID int64) *Vec3 {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil
	}
	pos := p.Pos
	return &pos
}

// PlayerState returns a snapshot copy of a player (nil if absent).
func (s *Sim) PlayerState(charID int64) *PlayerState {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil
	}
	return &PlayerState{
		ID:          p.ID,
		AccountID:   p.AccountID,
		CharacterID: p.CharacterID,
		Zone:        p.Zone,
		Pos:         p.Pos,
	}
}

// PlayerCount returns the number of in-world players (health/CCU).
func (s *Sim) PlayerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.players)
}

// SetMove enqueues the latest validated movement intent for a player.
// Returns ErrBadMove on validation failure. The intent replaces the
// previous one (latest-wins — appropriate for 20 Hz movement).
func (s *Sim) SetMove(charID int64, m MoveIntent) error {
	// Validate outside the lock (cheap, no shared state touched).
	if m.Speed < 0 || m.Speed > 1000 {
		return ErrBadMove
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	p.pending = m
	return nil
}

// SavePlayerPositions persists every in-world player's current position via
// SavePos (write-behind flush). Called by the gameserver on a timer and on
// graceful drain.
func (s *Sim) SavePlayerPositions(ctx context.Context) {
	s.mu.Lock()
	pos := make(map[int64]Vec3, len(s.players))
	for _, p := range s.players {
		pos[p.CharacterID] = p.Pos
	}
	s.mu.Unlock()
	if s.SavePos == nil {
		return
	}
	for charID, p := range pos {
		if err := s.SavePos(ctx, charID, p); err != nil {
			s.log("warn: save pos char=%d: %v", charID, err)
		}
	}
}

// tickOnce advances the simulation by one tick and emits snapshots.
func (s *Sim) tickOnce(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, p := range s.players {
		s.applyMove(p)
	}
	for _, p := range s.players {
		s.emitSnapshot(p)
	}
}

// applyMove integrates the player's pending intent for one tick (dt).
func (s *Sim) applyMove(p *Player) {
	m := p.pending
	dt := s.tick.Seconds()

	// Speed clamp: never faster than MaxSpeed. Log offenders.
	speed := m.Speed
	if speed > p.MaxSpeed {
		s.log("warn: speed clamp char=%d req=%.1f max=%.1f", p.CharacterID, speed, p.MaxSpeed)
		speed = p.MaxSpeed
	}
	if speed <= 0 {
		if p.Moving {
			p.Speed = 0
			p.Moving = false
			p.dirtySelf = true
		}
		return
	}

	var delta Vec3
	if m.Target != nil {
		// Click-to-move: move toward the target at clamped speed.
		to := m.Target.Sub(p.Pos)
		dist := to.Len()
		if dist < 0.001 {
			p.Speed = 0
			p.Moving = false
			p.dirtySelf = true
			return
		}
		step := speed * dt
		if step >= dist {
			p.Pos = *m.Target
			p.Speed = 0
			p.Moving = false
			p.dirtySelf = true
			return
		}
		dir := to.Normalize()
		delta = dir.Mul(step)
		p.RotY = atan2(dir.Z, dir.X)
		p.Speed = speed
		p.Moving = true
	} else if m.Direction.Len() > 0 {
		dir := m.Direction.Normalize()
		delta = dir.Mul(speed * dt)
		p.RotY = atan2(dir.Z, dir.X)
		p.Speed = speed
		p.Moving = true
	} else {
		if p.Moving {
			p.Speed = 0
			p.Moving = false
			p.dirtySelf = true
		}
		return
	}

	// Move, clamped to zone bounds.
	newPos := p.Pos.Add(delta)
	if z := s.zones[p.Zone]; z != nil {
		halfX, halfZ := z.SizeX/2, z.SizeZ/2
		if newPos.X < -halfX {
			newPos.X = -halfX
		}
		if newPos.X > halfX {
			newPos.X = halfX
		}
		if newPos.Z < -halfZ {
			newPos.Z = -halfZ
		}
		if newPos.Z > halfZ {
			newPos.Z = halfZ
		}
	}
	p.Pos = newPos
	p.dirtySelf = true
	s.grid.Refresh(&p.Entity)
}

// emitSnapshot computes the player's AOI delta and pushes a WorldSnapshot.
func (s *Sim) emitSnapshot(p *Player) {
	if p.Outbox == nil {
		return
	}
	near := s.grid.Nearby(p.Pos, p.ID)
	snap := &aet.WorldSnapshot{
		SelfId:     p.ID,
		Self:       []*aet.EntityState{},
		Entities:   []*aet.EntityState{},
		DespawnIds: []uint64{},
	}

	if p.dirtySelf {
		snap.Self = append(snap.Self, p.State())
		p.dirtySelf = false
	}

	nowKnown := make(map[uint64]bool, len(near))
	for _, e := range near {
		nowKnown[e.ID] = true
		state := e.State()
		last, seen := p.known[e.ID]
		if !seen || entityMoved(last, state) {
			snap.Entities = append(snap.Entities, state)
		}
		p.known[e.ID] = state
	}
	for id := range p.known {
		if !nowKnown[id] {
			snap.DespawnIds = append(snap.DespawnIds, id)
			delete(p.known, id)
		}
	}

	if len(snap.Self) == 0 && len(snap.Entities) == 0 && len(snap.DespawnIds) == 0 {
		return
	}
	frame, err := proto.Marshal(&aet.Envelope{
		Kind:        aet.Envelope_KIND_EVENT,
		PayloadType: "aetheria.WorldSnapshot",
		Payload:     mustMarshal(snap),
	})
	if err != nil {
		s.log("warn: marshal snapshot envelope: %v", err)
		return
	}
	select {
	case p.Outbox <- frame:
	default:
		// Slow consumer: drop this frame; 20 Hz cadence recovers next tick.
	}
}

// entityMoved reports whether a newer state differs from what was last sent.
func entityMoved(last, cur *aet.EntityState) bool {
	return last.Position.X != cur.Position.X ||
		last.Position.Y != cur.Position.Y ||
		last.Position.Z != cur.Position.Z ||
		last.Hp != cur.Hp ||
		last.IsMoving != cur.IsMoving
}

// mustMarshal panics on marshal failure (frames are small, fixed shapes).
func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
