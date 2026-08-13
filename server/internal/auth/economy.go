// Economy persistence (brief §212, M4): item instances and the audited gold
// ledger. Every gold mutation flows through ApplyGoldLedger which updates
// characters.gold and inserts a gold_ledger row in one transaction, so
// sum(gold_ledger) == sum(characters.gold) holds by construction.
package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/itsbaldeep/aetheria/server/internal/world"
)

// LedgerEntry mirrors world.LedgerEntry (a signed gold delta + reason).
type LedgerEntry struct {
	CharID int64
	Amount int64
	Reason string
}

// ApplyGoldLedger applies a batch of signed gold deltas to characters and
// writes one gold_ledger row per entry in a single transaction (M4). A
// character's balance never goes negative: the UPDATE ... RETURNING skips rows
// that would drop below 0, and those entries are returned as rejected.
func (s *Store) ApplyGoldLedger(ctx context.Context, entries []world.LedgerEntry) ([]world.LedgerEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: ledger begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var rejected []world.LedgerEntry
	for _, e := range entries {
		var newGold int64
		err := tx.QueryRow(ctx,
			`UPDATE characters SET gold = gold + $2, updated_at = now()
			 WHERE id = $1 AND gold + $2 >= 0 RETURNING gold`,
			e.CharID, e.Amount).Scan(&newGold)
		if err == pgx.ErrNoRows {
			rejected = append(rejected, e)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("auth: ledger update char %d: %w", e.CharID, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO gold_ledger (char_id, amount, reason) VALUES ($1, $2, $3)`,
			e.CharID, e.Amount, e.Reason); err != nil {
			return nil, fmt.Errorf("auth: ledger insert: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("auth: ledger commit: %w", err)
	}
	return rejected, nil
}

// WorldGoldSum returns the total gold across all characters (acceptance: the
// ledger must reconcile to this).
func (s *Store) WorldGoldSum(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(gold), 0) FROM characters WHERE deleted_at IS NULL`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("auth: world gold sum: %w", err)
	}
	return total, nil
}

// LedgerSum returns the sum of all gold_ledger amounts (acceptance invariant).
func (s *Store) LedgerSum(ctx context.Context) (int64, error) {
	var total int64
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(SUM(amount), 0) FROM gold_ledger`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("auth: ledger sum: %w", err)
	}
	return total, nil
}

// CharacterItem is one persisted item instance row (mirrors item_instances).
type CharacterItem struct {
	ID        uint64
	DefID     string
	Qty       int32
	Bound     bool
	Container string // inventory|equipment
	Slot      int32
	Stats     map[string]int64
}

// LoadCharacterItems loads a character's item instances (M4).
func (s *Store) LoadCharacterItems(ctx context.Context, charID int64) ([]world.Item, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, item_def_id, quantity, bound, COALESCE(rolled_stats, '{}'::jsonb)
		 FROM item_instances WHERE owner_char_id = $1 ORDER BY container, slot`, charID)
	if err != nil {
		return nil, fmt.Errorf("auth: load items: %w", err)
	}
	defer rows.Close()
	var items []world.Item
	for rows.Next() {
		var it world.Item
		var id uint64
		if err := rows.Scan(&id, &it.DefID, &it.Qty, &it.Bound, &it.Stats); err != nil {
			return nil, fmt.Errorf("auth: scan item: %w", err)
		}
		it.ID = id
		if it.Stats == nil {
			it.Stats = map[string]int64{}
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// SaveCharacterItems replaces a character's item instances in one transaction
// (delete + insert). Container/slot encode grid position and equipment slot.
func (s *Store) SaveCharacterItems(ctx context.Context, charID int64, items []world.Item) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: items begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM item_instances WHERE owner_char_id = $1`, charID); err != nil {
		return fmt.Errorf("auth: items delete: %w", err)
	}
	for _, it := range items {
		container := "inventory"
		var slot int32
		if it.Container != "" {
			container = it.Container
		}
		slot = it.Slot
		if _, err := tx.Exec(ctx,
			`INSERT INTO item_instances (owner_char_id, container, slot, item_def_id, quantity, bound, rolled_stats)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			charID, container, slot, it.DefID, it.Qty, it.Bound, it.Stats); err != nil {
			return fmt.Errorf("auth: items insert: %w", err)
		}
	}
	return tx.Commit(ctx)
}
