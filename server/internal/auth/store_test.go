package auth

import (
	"context"
	"os"
	"testing"
	"time"
)

// These tests exercise the real Postgres + Redis. They run only when
// AETHERIA_PG_DSN is set (the deploy/bottest path); `make test` in a bare
// checkout skips them so CI stays hermetic. The bot register scenario in
// tools/botclient exercises the same code end-to-end against the live DB.
func TestRegisterAndLoginIntegration(t *testing.T) {
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

	email := "itest-" + time.Now().Format("20060102T150405") + "@aetheria.test"
	hash, err := HashPassword("integration-pass-9")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	id, err := store.Register(ctx, email, hash)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if id == 0 {
		t.Fatal("Register returned id 0")
	}

	// Duplicate email → ErrEmailTaken.
	if _, err := store.Register(ctx, email, hash); err != ErrEmailTaken {
		t.Fatalf("Register(dup) = %v, want ErrEmailTaken", err)
	}

	// Credentials round-trip.
	gotID, gotHash, banned, err := store.Credentials(ctx, email)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if gotID != id {
		t.Fatalf("Credentials id = %d, want %d", gotID, id)
	}
	if !VerifyPassword(gotHash, "integration-pass-9") {
		t.Fatal("stored hash must verify the original password")
	}
	if banned != nil {
		t.Fatalf("fresh account must not be banned, got %v", banned)
	}

	// Unknown email → (0, nil, nil).
	uid, uh, ub, err := store.Credentials(ctx, "nobody@aetheria.test")
	if err != nil || uid != 0 || uh != "" || ub != nil {
		t.Fatalf("Credentials(unknown) = (%d,%q,%v,%v), want (0,\"\",nil,nil)", uid, uh, ub, err)
	}
}
