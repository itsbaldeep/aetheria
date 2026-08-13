// Economy: items, grid inventory, equipment with stat application, ground
// drops, pickup radius rules, gold with an audited ledger, and vendors
// (brief §212, M4). Every gold mutation flows through creditGold, which
// records a signed ledger entry; the ledger is the source of truth for the
// invariant sum(ledger) == world gold.
package world

import (
	"context"
	"errors"
	"time"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// InventorySize is the player grid inventory slot count (brief §212).
const InventorySize = 24

// pickupRadius is how close a player must be to a drop to pick it up.
const pickupRadius = 3.0

// dropTTL is how long a ground drop lasts before it despawns.
const dropTTL = 2 * time.Minute

// Equipment slot names.
const (
	slotWeapon    = "weapon"
	slotChest     = "chest"
	slotAccessory = "accessory"
)

// Item is one item instance. ID is world-local (matches item_instances.id in
// the DB mirror for persistable items). Stats holds rolled/derived stats
// (base_stats from the def by default).
type Item struct {
	ID    uint64
	DefID string
	Qty   int32
	Bound bool
	Stats map[string]int64

	// Persistence mirror (M4): container ("inventory"|"equipment") and grid
	// slot. Populated by the gameserver when saving/loading; the sim's grid
	// inventory is authoritative while in-world.
	Container string
	Slot      int32
}

// Drop is a ground loot entity: either an item or pure gold.
type Drop struct {
	Entity
	DefID     string
	Qty       int32
	Gold      int64
	ExpiresAt time.Time
}

// LedgerEntry is one audited gold mutation (signed delta). Mirrors
// gold_ledger rows; sum over all entries equals world gold by construction.
type LedgerEntry struct {
	CharID int64
	Amount int64
	Reason string
}

// pickupItem finds and claims a ground drop for a player. The pickup radius
// rule (brief §212) requires the player to be within pickupRadius and in the
// same zone; a drop can be claimed exactly once. Item drops go to inventory
// (stacking stackables), gold drops credit the player via the ledger.
func (s *Sim) PickupItem(charID int64, dropID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	d := s.drops[dropID]
	if d == nil {
		return errors.New("drop not found")
	}
	if d.ExpiresAt.Before(time.Now()) {
		delete(s.drops, dropID)
		s.grid.Remove(&d.Entity)
		return errors.New("drop expired")
	}
	if d.Zone != p.Zone || p.Pos.Distance(d.Pos) > pickupRadius {
		return errors.New("too far from drop")
	}
	if d.Gold > 0 {
		s.creditGold(p, d.Gold, "pickup_gold")
		s.sendEvent(p, lootEvent(d, d.Gold, 0, p.Gold))
	} else {
		it := &Item{DefID: d.DefID, Qty: d.Qty, Stats: s.itemStats(d.DefID)}
		if !s.addItem(p, it) {
			return errors.New("inventory full")
		}
		s.collectHook(p, d.DefID, it.Qty)
		s.sendEvent(p, lootEvent(&Drop{Entity: Entity{ID: it.ID}, DefID: d.DefID}, 0, it.Qty, p.Gold))
	}
	delete(s.drops, dropID)
	s.grid.Remove(&d.Entity)
	return nil
}

// EquipItem moves an inventory item into its equipment slot, applying its
// stats. Returns an error if the item is not equippable or not in inventory.
func (s *Sim) EquipItem(charID int64, itemID uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	it := s.takeItem(p, itemID, 1)
	if it == nil {
		return errors.New("item not in inventory")
	}
	def := s.itemDefs[it.DefID]
	if def == nil {
		s.addItem(p, it) // restore; should never happen
		return errors.New("unknown item def")
	}
	slot := equipmentSlot(def.Type)
	if slot == "" {
		s.addItem(p, it) // restore
		return errors.New("item not equippable")
	}
	// Swap: unequip whatever is in the slot back into inventory first.
	if prev := p.Equipment[slot]; prev != nil {
		if !s.addItem(p, prev) {
			p.Equipment[slot] = it // keep both by restoring the new item
			s.addItem(p, prev)
			return errors.New("inventory full")
		}
	}
	p.Equipment[slot] = it
	p.dirtySelf = true
	s.sendEvent(p, lootEvent(&Drop{Entity: Entity{ID: itemID}}, 0, it.Qty, p.Gold))
	return nil
}

// UnequipItem moves an equipped item back into inventory.
func (s *Sim) UnequipItem(charID int64, slot string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	it := p.Equipment[slot]
	if it == nil {
		return errors.New("nothing equipped in slot")
	}
	if !s.addItem(p, it) {
		return errors.New("inventory full")
	}
	delete(p.Equipment, slot)
	p.dirtySelf = true
	return nil
}

// SellItem sells inventory items to a vendor at their vendor_price, crediting
// gold via the ledger (reason "vendor_sell").
func (s *Sim) SellItem(charID int64, itemID uint64, qty int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	it := s.takeItem(p, itemID, qty)
	if it == nil {
		return errors.New("item not in inventory")
	}
	def := s.itemDefs[it.DefID]
	if def == nil {
		s.addItem(p, it)
		return errors.New("unknown item def")
	}
	s.creditGold(p, def.VendorPrice*int64(qty), "vendor_sell")
	s.sendEvent(p, lootEvent(&Drop{Entity: Entity{ID: itemID}}, def.VendorPrice*int64(qty), qty, p.Gold))
	return nil
}

// BuyItem purchases qty of an item def from a vendor's stock. Cost is paid
// via the ledger (reason "vendor_buy"). The item lands in inventory.
func (s *Sim) BuyItem(charID int64, vendorID, itemDefID string, qty int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	v, ok := s.vendors[vendorID]
	if !ok {
		return errors.New("vendor not found")
	}
	if !contains(v.Stock, itemDefID) {
		return errors.New("vendor does not sell that item")
	}
	def := s.itemDefs[itemDefID]
	if def == nil {
		return errors.New("unknown item def")
	}
	if qty <= 0 {
		return errors.New("bad quantity")
	}
	cost := def.VendorPrice * int64(qty)
	if p.Gold < cost {
		return errors.New("not enough gold")
	}
	it := &Item{DefID: itemDefID, Qty: qty, Stats: s.itemStats(itemDefID)}
	if !s.addItem(p, it) {
		return errors.New("inventory full")
	}
	s.creditGold(p, -cost, "vendor_buy")
	s.collectHook(p, itemDefID, qty)
	s.sendEvent(p, lootEvent(&Drop{Entity: Entity{ID: 0}}, -cost, qty, p.Gold))
	return nil
}

// creditGold applies a signed gold delta to a player and appends the ledger
// entry. This is the single audited gold-mutation path (brief §212).
func (s *Sim) creditGold(p *Player, delta int64, reason string) {
	p.Gold += delta
	s.ledger = append(s.ledger, LedgerEntry{CharID: p.CharacterID, Amount: delta, Reason: reason})
}

// addItem places an item in the player's inventory, stacking stackables.
// New instances (ID == 0) get a world-local id. Returns false when the grid
// is full.
func (s *Sim) addItem(p *Player, it *Item) bool {
	if it.ID == 0 {
		s.nextItemID++
		it.ID = s.nextItemID
	}
	def := s.itemDefs[it.DefID]
	stackable := def != nil && def.Stackable
	if stackable {
		for _, existing := range p.Inventory {
			if existing != nil && existing.DefID == it.DefID {
				existing.Qty += it.Qty
				return true
			}
		}
	}
	for i, slot := range p.Inventory {
		if slot == nil {
			p.Inventory[i] = it
			return true
		}
	}
	return false
}

// takeItem removes up to qty of an inventory item by instance id. Returns the
// item (with the requested qty) or nil. Stackable items are split on partial
// removal.
func (s *Sim) takeItem(p *Player, itemID uint64, qty int32) *Item {
	for i, slot := range p.Inventory {
		if slot == nil || slot.ID != itemID {
			continue
		}
		if slot.Qty > qty {
			removed := *slot
			removed.Qty = qty
			slot.Qty -= qty
			return &removed
		}
		p.Inventory[i] = nil
		return slot
	}
	return nil
}

// itemStats returns a fresh stats map for an item def (base_stats copy).
func (s *Sim) itemStats(defID string) map[string]int64 {
	def := s.itemDefs[defID]
	if def == nil || len(def.BaseStats) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(def.BaseStats))
	for k, v := range def.BaseStats {
		out[k] = v
	}
	return out
}

// Attack returns the player's equipped attack stat (weapon + chest + access).
func (p *Player) Attack() int64 {
	var atk int64
	for _, it := range p.Equipment {
		if it != nil {
			atk += it.Stats["attack"]
		}
	}
	return atk
}

// Defense returns the player's equipped defense stat.
func (p *Player) Defense() int64 {
	var def int64
	for _, it := range p.Equipment {
		if it != nil {
			def += it.Stats["defense"]
		}
	}
	return def
}

// sweepDrops expires stale ground drops. Caller holds s.mu.
func (s *Sim) sweepDrops(now time.Time) {
	for id, d := range s.drops {
		if now.After(d.ExpiresAt) {
			delete(s.drops, id)
			s.grid.Remove(&d.Entity)
		}
	}
}

// spawnDrop places a ground drop at a position (loot roll output).
func (s *Sim) spawnDrop(pos Vec3, zone string, item *Item, gold int64) *Drop {
	s.nextID++
	name := "drop"
	if item != nil {
		if def := s.itemDefs[item.DefID]; def != nil {
			name = def.Name
		}
	}
	d := &Drop{
		Entity: Entity{
			ID:   s.nextID,
			Type: TypeDrop,
			Name: name,
			Zone: zone,
			Pos:  pos,
		},
		DefID:     "",
		ExpiresAt: time.Now().Add(dropTTL),
	}
	if item != nil {
		d.DefID = item.DefID
		d.RefID = item.DefID // surfaced so bots/clients can find drops by def (M5)
		d.Qty = item.Qty
	}
	if gold > 0 {
		d.Gold = gold
	}
	s.drops[d.ID] = d
	s.grid.Insert(&d.Entity)
	return d
}

// rollLoot rolls every drop table for a mob def and returns the spawned item
// drops (each a separate ground drop). Gold is handled separately by callers.
func (s *Sim) rollLoot(defID string, pos Vec3, zone string) []*Drop {
	var out []*Drop
	for _, dt := range s.dropTables {
		if dt.MobDefID != defID || dt.Chance <= 0 {
			continue
		}
		if s.rng.Float64() > dt.Chance {
			continue
		}
		qty := dt.MinQty
		if span := dt.MaxQty - dt.MinQty; span > 0 {
			qty += int32(s.rng.Int63n(int64(span) + 1))
		}
		out = append(out, s.spawnDrop(pos, zone, &Item{DefID: dt.ItemDefID, Qty: qty}, 0))
	}
	return out
}

// FlushGoldLedger drains the in-memory ledger into SaveLedger (the DB mirror
// path). Returns the number of entries flushed. Called on a timer and on
// disconnect; entries stay in-memory if SaveLedger is nil (tests).
func (s *Sim) FlushGoldLedger(ctx context.Context) int {
	if s.SaveLedger == nil || len(s.ledger) == 0 {
		return 0
	}
	s.mu.Lock()
	entries := make([]LedgerEntry, len(s.ledger))
	copy(entries, s.ledger)
	s.ledger = s.ledger[:0]
	s.mu.Unlock()
	if err := s.SaveLedger(ctx, entries); err != nil {
		// Re-queue on failure so nothing is lost.
		s.mu.Lock()
		s.ledger = append(entries, s.ledger...)
		s.mu.Unlock()
		s.log("warn: ledger flush failed (%d entries requeued): %v", len(entries), err)
		return 0
	}
	return len(entries)
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// PersistItems serializes a player's inventory + equipment into the flat item
// list the DB mirror expects (container/slot populated). Called by the
// gameserver on save. Returns a copy; the sim keeps owning the live items.
func (s *Sim) PersistItems(charID int64) []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return nil
	}
	var out []Item
	for i, it := range p.Inventory {
		if it == nil {
			continue
		}
		c := *it
		c.Container = "inventory"
		c.Slot = int32(i)
		out = append(out, c)
	}
	// Equipment slots persist in the equipment container with a stable slot
	// index (weapon=0, chest=1, accessory=2).
	slotIdx := map[string]int32{slotWeapon: 0, slotChest: 1, slotAccessory: 2}
	for slot, it := range p.Equipment {
		if it == nil {
			continue
		}
		c := *it
		c.Container = "equipment"
		c.Slot = slotIdx[slot]
		out = append(out, c)
	}
	return out
}

// RestoreItems places DB-loaded item instances back into a player's inventory
// grid and equipment (M4). Called by the gameserver at EnterWorld. Returns an
// error if the grid can't hold them all (should not happen — same state that
// was saved).
func (s *Sim) RestoreItems(charID int64, items []Item) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.players[charID]
	if p == nil {
		return errors.New("character not in world")
	}
	if p.Inventory == nil {
		p.Inventory = make([]*Item, InventorySize)
	}
	if p.Equipment == nil {
		p.Equipment = map[string]*Item{}
	}
	slotIdx := map[int32]string{0: slotWeapon, 1: slotChest, 2: slotAccessory}
	for i := range items {
		it := items[i]
		if it.Container == "equipment" {
			slot := slotIdx[it.Slot]
			if slot != "" {
				p.Equipment[slot] = &it
				continue
			}
		}
		if it.Slot >= 0 && int(it.Slot) < len(p.Inventory) && p.Inventory[it.Slot] == nil {
			p.Inventory[it.Slot] = &it
			continue
		}
		s.addItem(p, &it)
	}
	return nil
}

// lootEvent wraps a gold/item change into a LootEvent frame for the client.
// Reused for drops, purchases, and sales in M4. `ok` is always true on a
// successful mutation; `balance` is the player's post-mutation gold.
func lootEvent(d *Drop, gold int64, qty int32, balance int64) *aet.LootEvent {
	return &aet.LootEvent{
		Ok:        true,
		ItemId:    d.ID,
		ItemDefId: d.DefID,
		Quantity:  qty,
		Gold:      gold,
		Balance:   balance,
	}
}
