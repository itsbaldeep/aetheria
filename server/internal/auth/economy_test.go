package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/itsbaldeep/aetheria/server/internal/world"
)

// TestGoldLedgerConsistency verifies the M4 acceptance invariant against a
// real Postgres: after a batch of gold mutations, sum(gold_ledger) equals
// sum(characters.gold). Runs only when AETHERIA_PG_DSN is set (deploy path).
func TestGoldLedgerConsistency(t *testing.T) {
	dsn := os.Getenv("AETHERIA_PG_DSN")
	if dsn == "" {
		t.Skip("AETHERIA_PG_DSN not set; integration covered by make bottest")
	}
	ctx := context.Background()
	store, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Random local-part suffix so repeated/parallel runs never collide on the
	// accounts.email unique key.
	rnd := make([]byte, 3)
	if _, err := rand.Read(rnd); err != nil {
		t.Fatalf("rand: %v", err)
	}
	email := "ledger-" + time.Now().Format("20060102T150405") + "-" + hex.EncodeToString(rnd) + "@aetheria.test"
	hash, err := HashPassword("ledger-pass-1")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	accID, err := store.Register(ctx, email, hash)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Create a character (auth server's own method).
	charID := registerTestCharacter(t, store, accID)

	ledgerBefore, err := store.LedgerSum(ctx)
	if err != nil {
		t.Fatalf("LedgerSum before: %v", err)
	}
	goldBefore, err := store.WorldGoldSum(ctx)
	if err != nil {
		t.Fatalf("WorldGoldSum before: %v", err)
	}

	// Simulate: +100 grant, -25 vendor buy, +25 vendor sell (net 100).
	entries := []world.LedgerEntry{
		{CharID: charID, Amount: 100, Reason: "gm_grant"},
		{CharID: charID, Amount: -25, Reason: "vendor_buy"},
		{CharID: charID, Amount: 25, Reason: "vendor_sell"},
	}
	if rejected, err := store.ApplyGoldLedger(ctx, entries); err != nil {
		t.Fatalf("ApplyGoldLedger: %v", err)
	} else if len(rejected) != 0 {
		t.Fatalf("rejected entries: %d", len(rejected))
	}

	// Invariant: sum(ledger) == sum(characters.gold).
	ledgerAfter, err := store.LedgerSum(ctx)
	if err != nil {
		t.Fatalf("LedgerSum after: %v", err)
	}
	goldAfter, err := store.WorldGoldSum(ctx)
	if err != nil {
		t.Fatalf("WorldGoldSum after: %v", err)
	}
	if ledgerAfter-ledgerBefore != goldAfter-goldBefore {
		t.Fatalf("delta ledger %d != delta world gold %d (invariant violated)",
			ledgerAfter-ledgerBefore, goldAfter-goldBefore)
	}
}

// TestApplyGoldLedgerRejectsNegative verifies a balance can't go negative.
func TestApplyGoldLedgerRejectsNegative(t *testing.T) {
	dsn := os.Getenv("AETHERIA_PG_DSN")
	if dsn == "" {
		t.Skip("AETHERIA_PG_DSN not set; integration covered by make bottest")
	}
	ctx := context.Background()
	store, err := NewStore(ctx, dsn)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	rnd := make([]byte, 3)
	if _, err := rand.Read(rnd); err != nil {
		t.Fatalf("rand: %v", err)
	}
	email := "ledger-neg-" + time.Now().Format("20060102T150405") + "-" + hex.EncodeToString(rnd) + "@aetheria.test"
	hash, _ := HashPassword("ledger-pass-1")
	accID, err := store.Register(ctx, email, hash)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	charID := registerTestCharacter(t, store, accID)

	rejected, err := store.ApplyGoldLedger(ctx, []world.LedgerEntry{
		{CharID: charID, Amount: -50, Reason: "spend"},
	})
	if err != nil {
		t.Fatalf("ApplyGoldLedger: %v", err)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected = %d, want 1 (no negative gold)", len(rejected))
	}
}

// registerTestCharacter creates a character row via the store's character
// methods if available; otherwise inserts directly. Kept minimal for the
// ledger integration test. Random suffix so parallel runs never collide on
// the characters.name unique key.
func registerTestCharacter(t *testing.T, store *Store, accID int64) int64 {
	t.Helper()
	ctx := context.Background()
	rnd := make([]byte, 3)
	if _, err := rand.Read(rnd); err != nil {
		t.Fatalf("rand: %v", err)
	}
	var id int64
	err := store.pool.QueryRow(ctx,
		`INSERT INTO characters (account_id, name, class)
		 VALUES ($1, $2, 'blade_dancer') RETURNING id`,
		accID, "ledgerchar"+time.Now().Format("150405")+hex.EncodeToString(rnd)).Scan(&id)
	if err != nil {
		t.Fatalf("insert character: %v", err)
	}
	return id
}
