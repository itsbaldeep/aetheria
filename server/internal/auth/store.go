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
