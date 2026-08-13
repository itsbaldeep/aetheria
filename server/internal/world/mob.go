// Mobs (brief §3, M3): hostile NPC entities with a state machine
// (idle → aggro → return), threat table, leash, and respawn. Mobs live in the
// same grid/AOI as players and stream via WorldSnapshot deltas like everyone
// else. Only the simulation goroutine mutates a Mob.
package world

import (
	"time"
)

// MobAIState values.
const (
	MobIdle   = "idle"
	MobAggro  = "aggro"
	MobReturn = "return"
)

// Mob is one hostile entity spawned from a MobDef.
type Mob struct {
	Entity
	DefID string

	// spawn is the anchor point the mob leashes to and respawns at.
	spawn Vec3
	// threat tracks damage dealt by each entity id (threat table).
	threat map[uint64]int64
	// state is the current AI state.
	state string
	// target is the current threat leader (0 = none).
	target uint64
	// homeTimer counts down while returning/leashed.
	leashTimer time.Duration

	// skillCooldowns tracks when each skill id is next castable (unix ms).
	skillCooldowns map[string]int64
	// respawnAt is when the mob respawns after death (0 = alive).
	respawnAt time.Time
	// alive is false while waiting to respawn.
	alive bool
	// castTimer counts down to the mob's next skill use in combat.
	castTimer time.Duration
}

// NewMob builds a fresh alive mob at a spawn position.
func NewMob(def *MobDef, id uint64, pos Vec3) *Mob {
	return &Mob{
		Entity: Entity{
			ID:    id,
			Type:  TypeMob,
			Name:  def.Name,
			Zone:  def.ZoneID,
			Pos:   pos,
			HP:    def.HP,
			MaxHP: def.HP,
			Level: int32(def.Level),
			RefID: def.ID,
		},
		DefID:          def.ID,
		spawn:          pos,
		threat:         map[uint64]int64{},
		state:          MobIdle,
		skillCooldowns: map[string]int64{},
		alive:          true,
	}
}

// Target returns the mob's current target entity id.
func (m *Mob) Target() uint64 { return m.target }
