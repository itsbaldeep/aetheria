// Combat: server-authoritative damage/mitigation formulas, skill validation
// (cooldown/range/cost), XP/leveling, and combat log events (brief §3, M3).
// Everything here is pure and unit-testable; the sim calls these helpers.
package world

import (
	"math"
)

// Formula constants (brief §3: deterministic, no randomness in damage).
const (
	// Damage scaling per power point and level-based mitigation floor.
	powerPerLevel = 0.05 // +5% power per attacker level
	armorPerLevel = 1.0  // mitigation per defender level
	shieldFactor  = 0.6  // mana shield absorbs 60% of damage

	// XP curve: xp_to_next(level) = 40 * level (levels 1-10 in M3).
	xpBasePerLevel = 40
)

// HitResult is the outcome of one damaging skill application.
type HitResult struct {
	Damage int64
	Crit   bool
	Mitig  int64
	Killed bool
}

// MaxXPForLevel returns the XP required to advance from `level` to level+1.
func MaxXPForLevel(level int32) int64 {
	if level < 1 {
		level = 1
	}
	return xpBasePerLevel * int64(level)
}

// RollDamage computes outgoing damage for a skill against a defender of the
// given level. Deterministic: base power scaled by attacker level, mitigated
// by defender level. Returns raw damage before shields.
func RollDamage(power int64, attackerLevel, defenderLevel int32) HitResult {
	scaled := float64(power) * (1 + float64(attackerLevel-1)*powerPerLevel)
	mitig := armorPerLevel * float64(defenderLevel-1)
	raw := scaled - mitig
	if raw < 1 {
		raw = 1 // always at least chip damage
	}
	return HitResult{Damage: int64(math.Round(raw))}
}

// ApplyShield reduces incoming damage by a mana shield. Returns the actual
// damage that hits HP and the shield amount consumed.
func ApplyShield(incoming, shield int64) (dmg, shieldUsed int64) {
	if shield <= 0 {
		return incoming, 0
	}
	absorb := int64(math.Round(float64(incoming) * shieldFactor))
	if absorb > shield {
		absorb = shield
	}
	return incoming - absorb, absorb
}

// XPToLevel returns true when accumulated XP crosses the level-up threshold.
// Excess XP carries over. A level never exceeds 10 in M3.
func XPToLevel(level int32, xp int64) (newLevel int32, remaining int64, leveled bool) {
	for level < 10 {
		need := MaxXPForLevel(level)
		if xp < need {
			break
		}
		xp -= need
		level++
		leveled = true
	}
	if level > 10 {
		level = 10
	}
	return level, xp, leveled
}
