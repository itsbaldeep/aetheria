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
	GoldReward  int64    `json:"gold_reward"` // flat gold on kill (M4)
	SpawnBand   int      `json:"spawn_band"`  // 1 = low-level band, 3 = high
	Instances   int      `json:"instances"`   // copies spawned (M5; default 1)
}

// ZoneContent mirrors Zone plus a respawn shrine position.
type ZoneContent struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Safe   bool    `json:"safe"`
	MinX   float64 `json:"min_x"`
	MaxX   float64 `json:"max_x"`
	MinZ   float64 `json:"min_z"`
	MaxZ   float64 `json:"max_z"`
	Shrine Vec3    `json:"shrine"`
}

// ItemDef is one item template (brief §212, M4). Parsed from
// shared/content/items/<id>.json. base_stats keys: attack, defense (others
// apply to later milestones). vendor_price is the buy/sell gold value.
type ItemDef struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Type        string           `json:"type"` // weapon|armor|accessory|consumable|misc
	Stackable   bool             `json:"stackable"`
	BaseStats   map[string]int64 `json:"base_stats"`
	VendorPrice int64            `json:"vendor_price"`
}

// equipmentSlot maps an item type to the equipment slot it occupies.
func equipmentSlot(itemType string) string {
	switch itemType {
	case "weapon":
		return "weapon"
	case "armor":
		return "chest"
	case "accessory":
		return "accessory"
	}
	return "" // not equippable
}

// DropTable is one loot row (brief §212). Chance is 0..1; qty is uniform in
// [min_qty, max_qty]. Parsed from shared/content/drops/<id>.json.
type DropTable struct {
	ID        string  `json:"id"`
	MobDefID  string  `json:"mob_def_id"`
	ItemDefID string  `json:"item_def_id"`
	Chance    float64 `json:"chance"`
	MinQty    int32   `json:"min_qty"`
	MaxQty    int32   `json:"max_qty"`
}

// NPC is a static non-hostile content definition (vendors land in M4, quest
// givers land in M5). Pos is the world position the NPC stands at in its zone;
// every NPC def spawns a static TypeNPC entity there so players can walk up and
// talk (NpcInteract, brief §141).
type NPC struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	ZoneID string   `json:"zone_id"`
	Kind   string   `json:"kind"`  // vendor|questgiver|trainer|banker|...
	Stock  []string `json:"stock"` // item def ids this vendor sells
	Dialog string   `json:"dialog"`
	Pos    Vec3     `json:"pos"` // world-space spawn position (x/z; y unused)
}

// QuestObjectiveType values (brief §141 objectives).
const (
	ObjectiveKill    = "kill"    // kill `count` of a mob def
	ObjectiveCollect = "collect" // acquire `count` of an item def
	ObjectiveTalk    = "talk"    // talk to `count` of an npc def
)

// QuestObjective is one step of a quest. Target depends on Type: a mob def id
// (kill), item def id (collect), or npc def id (talk).
type QuestObjective struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Count  int32  `json:"count"`
}

// QuestRewards are granted once at turn-in. Gold flows through creditGold
// (audited ledger, reason "quest_reward").
type QuestRewards struct {
	XP    int64             `json:"xp"`
	Gold  int64             `json:"gold"`
	Items []QuestRewardItem `json:"items"`
}

// QuestRewardItem is a reward item def id + quantity.
type QuestRewardItem struct {
	DefID string `json:"def_id"`
	Qty   int32  `json:"qty"`
}

// QuestDef is one quest (brief §141, M5). Parsed from
// shared/content/quests/<id>.json. A linear breadcrumb chain links quests via
// NextQuest (the next quest unlocks when this one turns in).
type QuestDef struct {
	ID         string           `json:"id"`
	Name       string           `json:"name"`
	MinLevel   int32            `json:"min_level"`
	GiverNPC   string           `json:"giver_npc"`  // npc def id that offers it
	TurninNPC  string           `json:"turnin_npc"` // npc def id that completes it
	Dialog     string           `json:"dialog"`     // giver flavor text
	Objectives []QuestObjective `json:"objectives"`
	Rewards    QuestRewards     `json:"rewards"`
	NextQuest  string           `json:"next_quest"`
}

// Content is the parsed set of content seeds.
type Content struct {
	Skills map[string]*SkillDef
	Mobs   map[string]*MobDef
	Zones  map[string]*ZoneContent
	Items  map[string]*ItemDef
	Drops  []*DropTable
	NPCs   map[string]*NPC
	Quests map[string]*QuestDef
}

// LoadContent reads every *.json file under shared/content/<kind>/ and
// returns a Content. Malformed or missing files are fatal errors: content is
// authoritative and a bad seed must fail loudly at startup.
func LoadContent(root string) (*Content, error) {
	c := &Content{
		Skills: map[string]*SkillDef{},
		Mobs:   map[string]*MobDef{},
		Zones:  map[string]*ZoneContent{},
		Items:  map[string]*ItemDef{},
		NPCs:   map[string]*NPC{},
		Quests: map[string]*QuestDef{},
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
	if err := loadDir(filepath.Join(root, "items"), c.Items); err != nil {
		return nil, err
	}
	if err := loadDir(filepath.Join(root, "npcs"), c.NPCs); err != nil {
		return nil, err
	}
	if err := loadDir(filepath.Join(root, "quests"), c.Quests); err != nil {
		return nil, err
	}
	if err := loadDropDir(filepath.Join(root, "drops"), c); err != nil {
		return nil, err
	}
	if len(c.Skills) == 0 || len(c.Mobs) == 0 || len(c.Zones) == 0 {
		return nil, fmt.Errorf("content: empty seed dirs (root=%s)", root)
	}
	return c, nil
}

// loadDropDir parses the drops/ directory: a plain list (each file holds one
// table object). Missing dir is tolerated for early checkouts; an empty one
// is a fatal error once the dir exists.
func loadDropDir(dir string, c *Content) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // drops not seeded yet (M4 accepts empty during dev)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("content: read %s: %w", e.Name(), err)
		}
		var t DropTable
		if err := json.Unmarshal(data, &t); err != nil {
			return fmt.Errorf("content: %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			return fmt.Errorf("content: %s: missing id", e.Name())
		}
		c.Drops = append(c.Drops, &t)
	}
	return nil
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
	case ItemDef:
		return t.ID
	case NPC:
		return t.ID
	case QuestDef:
		return t.ID
	}
	return ""
}
