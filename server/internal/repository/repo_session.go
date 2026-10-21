package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"probe/server/internal/model"
)

var ErrSessionNotFound = errors.New("session not found")

type SessionRepo struct {
	db *sql.DB
}

func NewSessionRepo(db *sql.DB) *SessionRepo {
	return &SessionRepo{db: db}
}

func (r *SessionRepo) Create(ctx context.Context, s *model.Session) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions
		(id, token_hash, admin_id, expires_at, idle_expires_at, revoked, created_at,
		 last_seen_at, ip, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.ID, s.TokenHash, s.AdminID, s.ExpiresAt.Unix(), s.IdleExpiresAt.Unix(),
		boolInt(s.Revoked), s.CreatedAt.Unix(), nullTime(s.LastSeenAt), s.IP, s.UserAgent)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (r *SessionRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Session, error) {
	var s model.Session
	var revoked int
	var created, expires, idle int64
	var lastSeen sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT id, token_hash, admin_id, expires_at,
		idle_expires_at, revoked, created_at, last_seen_at, ip, user_agent
		FROM sessions WHERE token_hash = ?`, tokenHash).Scan(
		&s.ID, &s.TokenHash, &s.AdminID, &expires, &idle, &revoked, &created,
		&lastSeen, &s.IP, &s.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	s.ExpiresAt = time.Unix(expires, 0)
	s.IdleExpiresAt = time.Unix(idle, 0)
	s.Revoked = revoked != 0
	s.CreatedAt = time.Unix(created, 0)
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0)
		s.LastSeenAt = &t
	}
	return &s, nil
}

// Touch extends the idle deadline and records activity. It never extends the
// absolute expiry.
func (r *SessionRepo) Touch(ctx context.Context, id string, idleExpiresAt, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions
		SET idle_expires_at = ?, last_seen_at = ? WHERE id = ? AND revoked = 0`,
		idleExpiresAt.Unix(), at.Unix(), id)
	if err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *SessionRepo) Revoke(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (r *SessionRepo) RevokeAll(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `UPDATE sessions SET revoked = 1 WHERE revoked = 0`)
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

func (r *SessionRepo) List(ctx context.Context) ([]model.Session, error) {
	now := time.Now().Unix()
	rows, err := r.db.QueryContext(ctx, `SELECT id, token_hash, admin_id, expires_at,
		idle_expires_at, revoked, created_at, last_seen_at, ip, user_agent
		FROM sessions WHERE revoked = 0 AND expires_at > ? AND idle_expires_at > ?
		ORDER BY created_at DESC`, now, now)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []model.Session
	for rows.Next() {
		var s model.Session
		var revoked int
		var created, expires, idle int64
		var lastSeen sql.NullInt64
		if err := rows.Scan(&s.ID, &s.TokenHash, &s.AdminID, &expires, &idle, &revoked,
			&created, &lastSeen, &s.IP, &s.UserAgent); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		s.ExpiresAt = time.Unix(expires, 0)
		s.IdleExpiresAt = time.Unix(idle, 0)
		s.Revoked = revoked != 0
		s.CreatedAt = time.Unix(created, 0)
		if lastSeen.Valid {
			t := time.Unix(lastSeen.Int64, 0)
			s.LastSeenAt = &t
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *SessionRepo) DeleteExpired(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR idle_expires_at <= ?`,
		now.Unix(), now.Unix())
	if err != nil {
		return fmt.Errorf("delete expired sessions: %w", err)
	}
	return nil
}
