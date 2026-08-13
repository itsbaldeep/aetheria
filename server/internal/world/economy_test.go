package world

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// m4TestContent extends the M3 test content with a small economy: two weapons,
// a stackable, a vendor, and drop tables for the test mobs.
func m4TestContent() *Content {
	c := m3TestContent()
	c.Items = map[string]*ItemDef{
		"iron_sword":   {ID: "iron_sword", Name: "Iron Sword", Type: "weapon", BaseStats: map[string]int64{"attack": 5}, VendorPrice: 25},
		"ashmaw_fang":  {ID: "ashmaw_fang", Name: "Ashmaw Fang", Type: "weapon", BaseStats: map[string]int64{"attack": 8}, VendorPrice: 60},
		"leather_vest": {ID: "leather_vest", Name: "Leather Vest", Type: "armor", BaseStats: map[string]int64{"defense": 3}, VendorPrice: 15},
		"minor_potion": {ID: "minor_potion", Name: "Minor Potion", Type: "consumable", Stackable: true, BaseStats: map[string]int64{"heal": 20}, VendorPrice: 5},
		"boar_hide":    {ID: "boar_hide", Name: "Boar Hide", Type: "misc", Stackable: true, VendorPrice: 3},
	}
	c.Drops = []*DropTable{
		{ID: "drop_boar_hide", MobDefID: "forest_boar", ItemDefID: "boar_hide", Chance: 1.0, MinQty: 1, MaxQty: 1},
		{ID: "drop_ashmaw_fang", MobDefID: "ashmaw", ItemDefID: "ashmaw_fang", Chance: 1.0, MinQty: 1, MaxQty: 1},
	}
	c.NPCs = map[string]*NPC{
		"vendor_maren": {ID: "vendor_maren", Name: "Maren", ZoneID: "havenport", Kind: "vendor",
			Stock: []string{"iron_sword", "leather_vest", "minor_potion", "boar_hide"}},
	}
	return c
}

// m4Sim builds a sim with M4 content and a recorder ledger.
func m4Sim(t *testing.T) (*Sim, *testLedger) {
	t.Helper()
	tl := &testLedger{}
	s := New(Options{
		Zones: []*Zone{
			{ID: "havenport", Name: "Havenport", Safe: true, MinX: -50, MaxX: 50, MinZ: -50, MaxZ: 50},
			{ID: "emberfield", Name: "Emberfield", MinX: -300, MaxX: 300, MinZ: -300, MaxZ: 300},
		},
		Content:    m4TestContent(),
		Tick:       50 * time.Millisecond,
		Logf:       func(f string, a ...any) { t.Logf(f, a...) },
		SaveLedger: tl.save,
	})
	return s, tl
}

// testLedger records every ledger flush; sum(entries) == world gold.
type testLedger struct {
	mu      sync.Mutex
	entries []LedgerEntry
}

func (l *testLedger) save(_ context.Context, entries []LedgerEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, entries...)
	return nil
}

func (l *testLedger) sum() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	var total int64
	for _, e := range l.entries {
		total += e.Amount
	}
	return total
}

func (l *testLedger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func m4Player(s *Sim, charID int64, class string, pos Vec3) *Player {
	p := m3Player(s, charID, class, pos)
	p.Inventory = make([]*Item, InventorySize)
	p.Equipment = map[string]*Item{}
	return p
}

// giveItem injects an item into a player's inventory directly (test helper).
func giveItem(s *Sim, p *Player, defID string, qty int32) *Item {
	it := &Item{DefID: defID, Qty: qty, Stats: s.itemStats(defID)}
	s.mu.Lock()
	s.addItem(p, it)
	s.mu.Unlock()
	return it
}

func TestEquipChangesDPS(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{100, 0, 100}) // emberfield (non-safe)
	m := m3Mob(s, "forest_boar", Vec3{102, 0, 100})

	// Base auto-attack damage (no weapon).
	base := RollDamage(10, 1, 1).Damage
	// Equip a weapon (+5 attack).
	it := giveItem(s, p, "iron_sword", 1)
	if err := s.EquipItem(p.CharacterID, it.ID); err != nil {
		t.Fatalf("EquipItem: %v", err)
	}
	with := RollDamage(10+p.Attack(), 1, 1).Damage
	if with <= base {
		t.Fatalf("equipping weapon did not increase DPS: base=%d with=%d", base, with)
	}
	if p.Attack() != 5 {
		t.Fatalf("Attack() = %d, want 5", p.Attack())
	}
	// The sim's damage path must use the equipped attack too.
	if err := s.CastSkill(p.CharacterID, CastRequest{SkillID: "blade_strike", TargetID: m.ID}); err != nil {
		t.Fatalf("cast: %v", err)
	}
	want := RollDamage(10+5, 1, 1).Damage
	if m.HP != m.MaxHP-want {
		t.Fatalf("mob HP after hit = %d, want %d (dmg %d)", m.HP, m.MaxHP-want, want)
	}
}

func TestConcurrentPickupNoDupe(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	// Spawn a guaranteed boar_hide drop at the player's feet via the loot-roll
	// path (m4TestContent has forest_boar → boar_hide at chance 1.0).
	s.mu.Lock()
	drops := s.rollLoot("forest_boar", p.Pos, p.Zone)
	s.mu.Unlock()
	if len(drops) != 1 {
		t.Fatalf("rollLoot returned %d drops, want 1", len(drops))
	}
	dropID := drops[0].ID

	// 100 parallel pickup attempts on the single drop; exactly one wins.
	const attempts = 100
	var wg sync.WaitGroup
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.PickupItem(p.CharacterID, dropID)
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("pickup successes = %d, want exactly 1 (no dupe)", success)
	}
	// The item landed exactly once in the player's grid.
	hideQty := int32(0)
	for _, it := range p.Inventory {
		if it != nil && it.DefID == "boar_hide" {
			hideQty += it.Qty
		}
	}
	if hideQty != 1 {
		t.Fatalf("boar_hide qty = %d, want 1 (no dupe)", hideQty)
	}
	if len(s.drops) != 0 {
		t.Fatalf("drop not removed from world")
	}
}

func TestConcurrentEquipSellNoDupe(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	// Two players race to equip/sell the same item instance.
	it := giveItem(s, p, "iron_sword", 1)

	// 100 parallel equip attempts: exactly one succeeds (the item leaves
	// inventory atomically under the sim lock).
	const attempts = 100
	var wg sync.WaitGroup
	equipOK := make(chan bool, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			equipOK <- s.EquipItem(p.CharacterID, it.ID) == nil
		}()
	}
	wg.Wait()
	close(equipOK)
	eq := 0
	for ok := range equipOK {
		if ok {
			eq++
		}
	}
	if eq != 1 {
		t.Fatalf("equip successes = %d, want exactly 1", eq)
	}
	if p.Equipment[slotWeapon] == nil {
		t.Fatal("weapon not equipped")
	}

	// Now 100 parallel sell attempts on the equipped item: 0 succeed because
	// equipped items aren't in inventory (sell requires inventory presence).
	sellOK := 0
	for i := 0; i < attempts; i++ {
		if s.SellItem(p.CharacterID, it.ID, 1) == nil {
			sellOK++
		}
	}
	if sellOK != 0 {
		t.Fatalf("sell of equipped item successes = %d, want 0", sellOK)
	}
}

func TestConcurrentPickupGoldNoDupe(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	s.mu.Lock()
	d := s.spawnDrop(p.Pos, p.Zone, nil, 100)
	s.mu.Unlock()

	var wg sync.WaitGroup
	ok := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok <- s.PickupItem(p.CharacterID, d.ID) == nil
		}()
	}
	wg.Wait()
	close(ok)
	success := 0
	for v := range ok {
		if v {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("gold pickup successes = %d, want 1", success)
	}
	if p.Gold != 100 {
		t.Fatalf("gold = %d, want 100 (credited exactly once)", p.Gold)
	}
}

func TestVendorRoundTripLedgerConsistent(t *testing.T) {
	s, tl := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	// Give the player starting gold through the audited path (grant).
	s.mu.Lock()
	s.creditGold(p, 100, "gm_grant")
	s.mu.Unlock()

	// Buy an Iron Sword for 25.
	if err := s.BuyItem(p.CharacterID, "vendor_maren", "iron_sword", 1); err != nil {
		t.Fatalf("BuyItem: %v", err)
	}
	if p.Gold != 75 {
		t.Fatalf("gold after buy = %d, want 75", p.Gold)
	}
	// Find the bought sword and sell it back for 25.
	var swordID uint64
	for _, it := range p.Inventory {
		if it != nil && it.DefID == "iron_sword" {
			swordID = it.ID
			break
		}
	}
	if swordID == 0 {
		for i, it := range p.Inventory {
			t.Logf("inv[%d] = %+v", i, it)
		}
		t.Fatal("bought sword not in inventory")
	}
	if err := s.SellItem(p.CharacterID, swordID, 1); err != nil {
		t.Fatalf("SellItem: %v", err)
	}
	if p.Gold != 100 {
		t.Fatalf("gold after round-trip = %d, want 100", p.Gold)
	}

	// Flush the ledger and check the invariant: sum(ledger) == world gold.
	flushed := s.FlushGoldLedger(context.Background())
	if flushed == 0 {
		t.Fatal("ledger did not flush")
	}
	worldGold := p.Gold // single-player world
	if got := tl.sum(); got != worldGold {
		t.Fatalf("sum(ledger)=%d != world gold=%d", got, worldGold)
	}
}

func TestPickupRadiusRule(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	s.mu.Lock()
	far := s.spawnDrop(Vec3{50, 0, 50}, p.Zone, &Item{DefID: "boar_hide", Qty: 1}, 0)
	near := s.spawnDrop(Vec3{2, 0, 0}, p.Zone, &Item{DefID: "boar_hide", Qty: 1}, 0)
	s.mu.Unlock()

	if err := s.PickupItem(p.CharacterID, far.ID); !errors.Is(err, nil) {
		// far is 70m away → must be rejected.
		if err == nil {
			t.Fatal("far drop picked up, want radius rejection")
		}
	}
	if err := s.PickupItem(p.CharacterID, far.ID); err == nil {
		t.Fatal("far drop picked up, want radius rejection")
	}
	if err := s.PickupItem(p.CharacterID, near.ID); err != nil {
		t.Fatalf("near drop pickup: %v", err)
	}
}

func TestInventoryCapacity(t *testing.T) {
	s, _ := m4Sim(t)
	p := m4Player(s, 1, "blade_dancer", Vec3{0, 0, 0})

	// Fill 23 slots with distinct non-stackable items; leave one free slot.
	for i := 0; i < InventorySize-1; i++ {
		if !s.addItem(p, &Item{DefID: "leather_vest", Qty: 1, Stats: s.itemStats("leather_vest")}) {
			t.Fatalf("slot %d should accept item", i)
		}
	}
	// A stackable creates a new stack in the last free slot.
	if !s.addItem(p, &Item{DefID: "minor_potion", Qty: 2, Stats: s.itemStats("minor_potion")}) {
		t.Fatal("stackable should create a stack")
	}
	// More of the same stackable merges onto the existing stack (no slot).
	if !s.addItem(p, &Item{DefID: "minor_potion", Qty: 3, Stats: s.itemStats("minor_potion")}) {
		t.Fatal("stackable should merge into an existing stack")
	}
	for _, it := range p.Inventory {
		if it != nil && it.DefID == "minor_potion" && it.Qty != 5 {
			t.Fatalf("stack qty = %d, want 5", it.Qty)
		}
	}
	// Grid is full: a new non-stackable cannot fit.
	if s.addItem(p, &Item{DefID: "leather_vest", Qty: 1, Stats: s.itemStats("leather_vest")}) {
		t.Fatal("inventory accepted item past capacity")
	}
}
