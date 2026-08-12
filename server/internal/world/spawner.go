// Mob spawner: places mob instances per content defs into banded areas of a
// zone (brief §3, M3). Spawn positions are deterministic so tests and soak
// runs get stable density. Only called at sim construction.
package world

import (
	"math"
	"sort"
)

// SpawnBands maps spawn_band → emberfield sub-areas. Band 1 is close to town
// (low level), band 3 is deep field (high level). Positions are absolute in
// the 600×600 emberfield zone.
var SpawnBands = map[int][]Vec3{
	1: {
		{70, 0, -60}, {120, 0, -40}, {60, 0, 40}, {110, 0, 90}, {40, 0, -110}, {140, 0, 120},
		{90, 0, -150}, {160, 0, -80}, {50, 0, 160}, {180, 0, 30},
	},
	2: {
		{-160, 0, 140}, {-110, 0, 180}, {-200, 0, 60}, {-140, 0, -100}, {-60, 0, 200},
		{-220, 0, -60}, {-90, 0, -180}, {-170, 0, -160}, {-240, 0, 120}, {-30, 0, -220},
	},
	3: {
		{-280, 0, -200}, {260, 0, -260}, {200, 0, 260}, {-250, 0, 250}, {280, 0, 140},
		{240, 0, -140}, {-180, 0, -280}, {300, 0, -40}, {-300, 0, 40}, {140, 0, 300},
	},
}

// SpawnMobs populates the sim's mobs map from content defs. Each mob def gets
// one instance at a banded spawn point (count = number of positions for that
// band modulo). Positions are assigned round-robin so mobs are spread out.
// Defs are iterated in sorted-id order so spawn placement is stable across
// restarts (a map iteration order would randomize where band-1 mobs land).
func SpawnMobs(s *Sim, content *Content, bands map[int][]Vec3) {
	positions := map[int][]Vec3{}
	if bands != nil {
		positions = bands
	}
	defs := make([]*MobDef, 0, len(content.Mobs))
	for _, def := range content.Mobs {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].ID < defs[j].ID })
	for _, def := range defs {
		band := def.SpawnBand
		if band == 0 {
			band = 1
		}
		locs := positions[band]
		if len(locs) == 0 {
			continue
		}
		pos := locs[len(s.mobs)%len(locs)]
		// Slight deterministic jitter so same-band mobs don't stack.
		j := float64(len(s.mobs) % 5)
		pos.X += math.Mod(j*7, 12) - 6
		pos.Z += math.Mod(j*11, 12) - 6
		s.spawnMob(def, pos)
	}
}

// spawnMob creates a mob at a position and registers it in the sim + grid.
func (s *Sim) spawnMob(def *MobDef, pos Vec3) {
	s.nextID++
	m := NewMob(def, s.nextID, pos)
	s.mobs[m.ID] = m
	s.grid.Insert(&m.Entity)
}

// MobCount returns the number of mobs currently registered (alive or not).
func (s *Sim) MobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mobs)
}
