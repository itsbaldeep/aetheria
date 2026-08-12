// Content loading: parses shared/content/*.json seeds (brief §4) into the
// world's skill/mob/zone definitions. The JSON files are the single source of
// truth; the DB mirrors them for admin/auction queries in later milestones.
package world

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillKind values (mirror skill_defs.kind).
const (
	SkillAuto      = "auto"      // basic attack, no cost/cooldown, tab-target
	SkillTabTarget = "tabtarget" // single enemy target
	SkillAimed     = "aimed"     // ground-AoE aimed at a position (telegraph)
	SkillPBAOE     = "pbaoe"     // point-blank area around the caster
	SkillSelf      = "self"      // buff on the caster
)

// SkillDef is one castable skill. Parsed from shared/content/skills/<id>.json.
type SkillDef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Class      string  `json:"class"` // class id, or "mob"
	Kind       string  `json:"kind"`
	Range      float64 `json:"range"`  // metres; 0 for self/pbaoe
	Radius     float64 `json:"radius"` // AoE radius for aimed/pbaoe
	CooldownMS int64   `json:"cooldown_ms"`
	CostMP     int64   `json:"cost_mp"`
	Power      int64   `json:"power"` // base damage / shield amount
}

// MobDef is one mob type. Parsed from shared/content/mobs/<id>.json.
type MobDef struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Level       int      `json:"level"`
	HP          int64    `json:"hp"`
	MP          int64    `json:"mp"`
	ZoneID      string   `json:"zone_id"`
	AggroRadius float64  `json:"aggro_radius"`
	LeashRadius float64  `json:"leash_radius"`
	Skills      []string `json:"skills"`
	XPReward    int64    `json:"xp_reward"`
	SpawnBand   int      `json:"spawn_band"` // 1 = low-level band, 3 = high
}

// ZoneContent mirrors Zone plus a respawn shrine position.
type ZoneContent struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Safe   bool    `json:"safe"`
	SizeX  float64 `json:"size_x"`
	SizeZ  float64 `json:"size_z"`
	Shrine Vec3    `json:"shrine"`
}

// Content is the parsed set of content seeds.
type Content struct {
	Skills map[string]*SkillDef
	Mobs   map[string]*MobDef
	Zones  map[string]*ZoneContent
}

// LoadContent reads every *.json file under shared/content/<kind>/ and
// returns a Content. Malformed or missing files are fatal errors: content is
// authoritative and a bad seed must fail loudly at startup.
func LoadContent(root string) (*Content, error) {
	c := &Content{
		Skills: map[string]*SkillDef{},
		Mobs:   map[string]*MobDef{},
		Zones:  map[string]*ZoneContent{},
	}
	if err := loadDir(filepath.Join(root, "skills"), c.Skills); err != nil {
		return nil, err
	}
	if err := loadDir(filepath.Join(root, "mobs"), c.Mobs); err != nil {
		return nil, err
	}
	if err := loadDir(filepath.Join(root, "zones"), c.Zones); err != nil {
		return nil, err
	}
	if len(c.Skills) == 0 || len(c.Mobs) == 0 || len(c.Zones) == 0 {
		return nil, fmt.Errorf("content: empty seed dirs (root=%s)", root)
	}
	return c, nil
}

func loadDir[T any](dir string, out map[string]*T) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("content: read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("content: read %s: %w", e.Name(), err)
		}
		var v T
		if err := json.Unmarshal(data, &v); err != nil {
			return fmt.Errorf("content: %s: %w", e.Name(), err)
		}
		// Get the id key generically via reflection-free re-parse on known types.
		id := entryID(v)
		if id == "" {
			return fmt.Errorf("content: %s: missing id", e.Name())
		}
		out[id] = &v
	}
	return nil
}

// entryID extracts the id field of a parsed content entry. Type switch keeps
// the helper dependency-free (no reflect).
func entryID(v any) string {
	switch t := v.(type) {
	case SkillDef:
		return t.ID
	case MobDef:
		return t.ID
	case ZoneContent:
		return t.ID
	}
	return ""
}
