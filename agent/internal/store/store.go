// Package store persists accounts and login sessions in a local SQLite
// database (pure-Go driver, no CGO) at a path controlled by the caller
// (typically /var/lib/rc-agent/agent.db).
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
)

var (
	ErrNotFound           = errors.New("store: not found")
	ErrUsernameTaken      = errors.New("store: username already taken")
	ErrInvalidCredentials = errors.New("store: invalid credentials")
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(1) // modernc sqlite: keep it simple, avoid concurrent-writer lock churn
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS users (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	username      TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	role          TEXT NOT NULL,
	created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sessions (
	token      TEXT PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	expires_at TIMESTAMP NOT NULL
);
`)
	return err
}

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CreateUser adds a new account. Returns ErrUsernameTaken if the name exists.
func (s *Store) CreateUser(ctx context.Context, username, password string, role model.Role) (*model.User, error) {
	if !role.Valid() {
		return nil, fmt.Errorf("store: invalid role %q", role)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, password_hash, role) VALUES (?, ?, ?)`,
		username, hash, string(role))
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite wraps sqlite3 error text; avoid importing its error type directly.
	return err != nil && (containsAny(err.Error(), "UNIQUE constraint failed", "constraint failed: UNIQUE"))
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

func (s *Store) GetUser(ctx context.Context, id int64) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, role, created_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	var role string
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &role, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Role = model.Role(role)
	return &u, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		var u model.User
		var role string
		if err := rows.Scan(&u.ID, &u.Username, &role, &u.CreatedAt); err != nil {
			return nil, err
		}
		u.Role = model.Role(role)
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword replaces a user's password hash.
func (s *Store) SetPassword(ctx context.Context, id int64, newPassword string) (*model.User, error) {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id); err != nil {
		return nil, err
	}
	return s.GetUser(ctx, id)
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// Authenticate verifies credentials and returns the matching user.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*model.User, error) {
	u, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Still run bcrypt to keep timing similar whether or not the user exists.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinuZ0000000000000000000000000"), []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return u, nil
}

const sessionTTL = 30 * 24 * time.Hour

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession issues a new session token for the given user.
func (s *Store) CreateSession(ctx context.Context, userID int64) (token string, expiresAt time.Time, err error) {
	token, err = newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().Add(sessionTTL)
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)`, token, userID, expiresAt)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// SessionUser resolves a session token to its user, or ErrNotFound if
// missing/expired (expired sessions are lazily deleted here).
func (s *Store) SessionUser(ctx context.Context, token string) (*model.User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT u.id, u.username, u.password_hash, u.role, u.created_at
FROM sessions s JOIN users u ON u.id = s.user_id
WHERE s.token = ? AND s.expires_at > ?`, token, time.Now())
	u, err := scanUser(row)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

// DeleteExpiredSessions is meant to be called periodically for housekeeping.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, time.Now())
	return err
}
