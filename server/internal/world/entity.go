// Entities in the world simulation. A player is an Entity with a live
// outbox channel; NPCs/mobs land in M3.
package world

import (
	"math"
	"sync/atomic"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// EntityType values (wire `entity_type`).
const (
	TypePlayer = "player"
	TypeNPC    = "npc"
	TypeMob    = "mob"
)

// Entity is one simulated object. Only the simulation goroutine mutates it;
// readers get a snapshot copy via State().
type Entity struct {
	ID     uint64
	Type   string
	Name   string
	Zone   string
	Pos    Vec3
	RotY   float64
	Speed  float64
	HP     int64
	MaxHP  int64
	Level  int32
	Moving bool

	// AOI bookkeeping: the grid cell this entity currently lives in.
	cell *cellKey
}

// State returns a wire EntityState snapshot of the entity.
func (e *Entity) State() *aet.EntityState {
	return &aet.EntityState{
		EntityId:   e.ID,
		EntityType: e.Type,
		Name:       e.Name,
		ZoneId:     e.Zone,
		Position: &aet.Vec3{
			X: float32(e.Pos.X),
			Y: float32(e.Pos.Y),
			Z: float32(e.Pos.Z),
		},
		RotY:     float32(e.RotY),
		Speed:    float32(e.Speed),
		Hp:       e.HP,
		MaxHp:    e.MaxHP,
		Level:    e.Level,
		IsMoving: e.Moving,
	}
}

// Player is a connected, in-world character. The wire layer owns Outbox
// lifecycle; the sim goroutine owns the rest.
type Player struct {
	Entity
	AccountID   int64
	CharacterID int64
	MaxSpeed    float64
	Class       string
	MP          int64
	MaxMP       int64
	XP          int64

	// Shield absorbs damage (mana_shield / defensive buffs).
	Shield int64

	// Outbox carries marshalled WorldSnapshot frames for this connection.
	Outbox chan []byte

	// Ready gates snapshot emission until the wire layer has enqueued the
	// EnterWorldAck, so the ack is always the first frame after EnterWorld.
	Ready atomic.Bool

	// Owned by the sim goroutine:
	known     map[uint64]*aet.EntityState
	dirtySelf bool
	pending   MoveIntent

	// Combat state (sim goroutine owns):
	target       uint64 // current target entity id (0 = none)
	autoAttack   uint64 // auto-attack target (0 = none)
	cooldowns    map[string]int64
	shieldExpiry time.Time
}

// PlayerState is a Player's public snapshot (for bots/health).
type PlayerState struct {
	ID          uint64
	AccountID   int64
	CharacterID int64
	Zone        string
	Pos         Vec3
}

// atan2 mirrors math.Atan2 for the sim's rotation math.
func atan2(z, x float64) float64 { return math.Atan2(z, x) }
