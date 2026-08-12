// Account store — Postgres-backed persistence for accounts. The only write
// path into the accounts table, so the authserver and portal stay consistent.
package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/itsbaldeep/aetheria/server/internal/world"
)

var ErrEmailTaken = errors.New("email already registered")

// Store persists accounts. All credential writes flow through here.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore opens a pgx pool. Caller owns Close.
func NewStore(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("auth: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("auth: ping db: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Register inserts a new account. Email must be pre-validated (lowercase).
// Returns ErrEmailTaken on a unique-violation so callers can map to HTTP 409.
func (s *Store) Register(ctx context.Context, email, passHash string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO accounts (email, pass_hash) VALUES ($1, $2) RETURNING id`,
		email, passHash).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrEmailTaken
		}
		return 0, fmt.Errorf("auth: insert account: %w", err)
	}
	return id, nil
}

// Credentials fetches account id + hash + ban status for login validation.
// Returns (0, nil, nil) when the email is unknown.
func (s *Store) Credentials(ctx context.Context, email string) (id int64, passHash string, bannedUntil *time.Time, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT id, pass_hash, banned_until FROM accounts WHERE email = $1`,
		email).Scan(&id, &passHash, &bannedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil, nil
	}
	if err != nil {
		return 0, "", nil, fmt.Errorf("auth: fetch account: %w", err)
	}
	return id, passHash, bannedUntil, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Login records a successful login (audit trail, brief §5 `logins`).
func (s *Store) Login(ctx context.Context, accountID int64, ip string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO logins (account_id, ip) VALUES ($1, $2)`, accountID, ip)
	if err != nil {
		return fmt.Errorf("auth: insert login: %w", err)
	}
	return nil
}

// BannedUntil returns the account's ban expiry, or nil if not banned.
// Used by the gameserver to re-check bans at WS handshake time (a token can
// outlive a fresh ban, so signature validity alone is insufficient).
func (s *Store) BannedUntil(ctx context.Context, accountID int64) (*time.Time, error) {
	var banned *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT banned_until FROM accounts WHERE id = $1`, accountID).Scan(&banned)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: fetch ban: %w", err)
	}
	return banned, nil
}

// CharacterSpawn is the data the gameserver needs to place a character in the
// world (M2). Zone/position come from the character's last saved state.
type CharacterSpawn struct {
	ID     int64
	Name   string
	Class  string
	ZoneID string
	Pos    worldVec
	Level  int32
	HP     int64
	MaxHP  int64
	MP     int64
	MaxMP  int64
	XP     int64
}

// worldVec aliases the world package's Vec3 so the auth package stays small.
type worldVec = world.Vec3

// LoadCharacterSpawn fetches a character's spawn state, verifying ownership
// (account must own the character). Returns nil, nil when not found.
func (s *Store) LoadCharacterSpawn(ctx context.Context, accountID, charID int64) (*CharacterSpawn, error) {
	var c CharacterSpawn
	var x, y, z float64
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, class, zone_id, pos_x, pos_y, pos_z, level, hp, mp,
		        COALESCE((stats->>'max_hp')::bigint, 100) AS max_hp,
		        COALESCE((stats->>'max_mp')::bigint, 50) AS max_mp, xp
		 FROM characters
		 WHERE id = $1 AND account_id = $2 AND deleted_at IS NULL`,
		charID, accountID).Scan(&c.ID, &c.Name, &c.Class, &c.ZoneID, &x, &y, &z, &c.Level, &c.HP, &c.MP, &c.MaxHP, &c.MaxMP, &c.XP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load spawn: %w", err)
	}
	c.Pos = worldVec{X: x, Y: y, Z: z}
	return &c, nil
}

// SaveCharacterPosition persists a character's current world position.
// Write-behind flush from the gameserver (brief §3: every 30 s + on logout).
func (s *Store) SaveCharacterPosition(ctx context.Context, charID int64, pos worldVec) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE characters SET pos_x = $1, pos_y = $2, pos_z = $3, updated_at = now()
		 WHERE id = $4`,
		pos.X, pos.Y, pos.Z, charID)
	if err != nil {
		return fmt.Errorf("auth: save position: %w", err)
	}
	return nil
}

// SaveCharacterState persists level/xp/hp/mp after combat events (M3). HP/MP
// current values are stored directly; max_hp/max_mp live in stats JSON so the
// world's derived values stay in sync with level-ups.
func (s *Store) SaveCharacterState(ctx context.Context, charID int64, level int32, xp, hp, mp int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE characters
		 SET level = $1, xp = $2, hp = $3, mp = $4,
		     stats = jsonb_set(jsonb_set(stats, '{max_hp}', to_jsonb($5::bigint)), '{max_mp}', to_jsonb($6::bigint)),
		     updated_at = now()
		 WHERE id = $7`,
		level, xp, hp, mp, maxHPForLevel(level), maxMPForLevel(level), charID)
	if err != nil {
		return fmt.Errorf("auth: save char state: %w", err)
	}
	return nil
}

// maxHPForLevel mirrors the world's level scaling for HP (M3).
func maxHPForLevel(level int32) int64 {
	if level < 1 {
		level = 1
	}
	return 100 + 20*int64(level-1)
}

// maxMPForLevel mirrors the world's level scaling for MP (M3).
func maxMPForLevel(level int32) int64 {
	if level < 1 {
		level = 1
	}
	return 50 + 10*int64(level-1)
}
