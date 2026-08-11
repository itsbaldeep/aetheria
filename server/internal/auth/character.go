// Character persistence + rules (brief §6: one Human race, two classes;
// §10: server validates name rules; soft-delete via deleted_at).
package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	ErrBadCharacterName = errors.New("character name must be 2-16 letters/digits/underscore")
	ErrBadClass         = errors.New("unknown class (choose blade_dancer or spellweaver)")
	ErrNameTaken        = errors.New("character name already taken")
	ErrCharLimit        = errors.New("account already has 6 characters")
)

// Allowed classes (brief §6). Values match the schema's `class` column.
const (
	ClassBladeDancer = "blade_dancer"
	ClassSpellweaver = "spellweaver"
)

// MaxCharsPerAccount bounds roster size (brief §5 soft-delete design).
const MaxCharsPerAccount = 6

var charNameRe = regexp.MustCompile(`^[A-Za-z0-9_]{2,16}$`)

// Character is a roster entry as exposed by the auth API.
type Character struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Class  string `json:"class"`
	Level  int    `json:"level"`
	ZoneID string `json:"zone_id"`
	IsNew  bool   `json:"is_new"`
}

// ValidCharacterClass reports whether class is one of the two MVP classes.
func ValidCharacterClass(class string) bool {
	return class == ClassBladeDancer || class == ClassSpellweaver
}

// ValidateCharacterName enforces the server-side name rules.
func ValidateCharacterName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if !charNameRe.MatchString(name) {
		return "", ErrBadCharacterName
	}
	return name, nil
}

// CreateCharacter inserts a new character under the account (soft-delete
// aware: a previously deleted name is reusable). Returns the new character.
func (s *Store) CreateCharacter(ctx context.Context, accountID int64, name, class string) (*Character, error) {
	// Roster bound: count only live characters.
	var live int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM characters WHERE account_id = $1 AND deleted_at IS NULL`,
		accountID).Scan(&live); err != nil {
		return nil, fmt.Errorf("auth: count chars: %w", err)
	}
	if live >= MaxCharsPerAccount {
		return nil, ErrCharLimit
	}

	// Reap the soft-deleted name first so names become reusable (there is a
	// UNIQUE index on name). Safe: a deleted row is invisible to the player.
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM characters WHERE name = $1 AND deleted_at IS NOT NULL`, name); err != nil {
		return nil, fmt.Errorf("auth: reap deleted char: %w", err)
	}

	var c Character
	err := s.pool.QueryRow(ctx,
		`INSERT INTO characters (account_id, name, class) VALUES ($1, $2, $3)
		 RETURNING id, name, class, level, zone_id`,
		accountID, name, class).Scan(&c.ID, &c.Name, &c.Class, &c.Level, &c.ZoneID)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, fmt.Errorf("auth: insert char: %w", err)
	}
	c.IsNew = true
	return &c, nil
}

// ListCharacters returns the account's live characters, newest first.
func (s *Store) ListCharacters(ctx context.Context, accountID int64) ([]Character, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, class, level, zone_id FROM characters
		 WHERE account_id = $1 AND deleted_at IS NULL ORDER BY id DESC`,
		accountID)
	if err != nil {
		return nil, fmt.Errorf("auth: list chars: %w", err)
	}
	defer rows.Close()

	var out []Character
	for rows.Next() {
		var c Character
		if err := rows.Scan(&c.ID, &c.Name, &c.Class, &c.Level, &c.ZoneID); err != nil {
			return nil, fmt.Errorf("auth: scan char: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCharacter fetches one live character by id + account (ownership check).
func (s *Store) GetCharacter(ctx context.Context, accountID, charID int64) (*Character, error) {
	var c Character
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, class, level, zone_id FROM characters
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		charID, accountID).Scan(&c.ID, &c.Name, &c.Class, &c.Level, &c.ZoneID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: get char: %w", err)
	}
	return &c, nil
}

// TouchCharacter bumps updated_at so the character's last-seen reflects the
// player picking it for a session (cheap; keeps the world honest later).
func (s *Store) TouchCharacter(ctx context.Context, charID int64) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE characters SET updated_at = now() WHERE id = $1`, charID); err != nil {
		return fmt.Errorf("auth: touch char: %w", err)
	}
	return nil
}
