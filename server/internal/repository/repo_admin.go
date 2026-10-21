package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"probe/server/internal/model"
)

var ErrAdminNotFound = errors.New("admin not found")

type AdminRepo struct {
	db *sql.DB
}

func NewAdminRepo(db *sql.DB) *AdminRepo {
	return &AdminRepo{db: db}
}

func (r *AdminRepo) Count(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count admins: %w", err)
	}
	return n, nil
}

func (r *AdminRepo) Create(ctx context.Context, a *model.Admin) (int64, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO admin
		(username, password_hash, totp_secret, totp_enabled, totp_recovery_codes, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		a.Username, a.PasswordHash, a.TOTPSecret, boolInt(a.TOTPEnabled), a.TOTPRecoveryCodes, a.CreatedAt.Unix())
	if err != nil {
		return 0, fmt.Errorf("insert admin: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("admin id: %w", err)
	}
	a.ID = id
	return id, nil
}

func (r *AdminRepo) GetByUsername(ctx context.Context, username string) (*model.Admin, error) {
	return r.get(ctx, `SELECT id, username, password_hash, totp_secret, totp_enabled,
		totp_recovery_codes, created_at FROM admin WHERE username = ?`, username)
}

func (r *AdminRepo) GetByID(ctx context.Context, id int64) (*model.Admin, error) {
	return r.get(ctx, `SELECT id, username, password_hash, totp_secret, totp_enabled,
		totp_recovery_codes, created_at FROM admin WHERE id = ?`, id)
}

// EnableTOTP writes the secret and recovery codes in one statement so a
// success can never leave TOTP enabled without recovery codes.
func (r *AdminRepo) EnableTOTP(ctx context.Context, id int64, secret, codes string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE admin SET totp_secret = ?, totp_enabled = 1,
		totp_recovery_codes = ? WHERE id = ?`, secret, codes, id); err != nil {
		return fmt.Errorf("enable admin TOTP: %w", err)
	}
	return nil
}

// SetRecoveryCodes replaces the remaining recovery-code hashes.
func (r *AdminRepo) SetRecoveryCodes(ctx context.Context, id int64, codes string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE admin SET totp_recovery_codes = ? WHERE id = ?`,
		codes, id); err != nil {
		return fmt.Errorf("set recovery codes: %w", err)
	}
	return nil
}

// ConsumeRecoveryCodes atomically replaces the recovery-code list only if it
// still matches the snapshot read during login. This prevents two concurrent
// logins from spending the same one-time code.
func (r *AdminRepo) ConsumeRecoveryCodes(ctx context.Context, id int64, original, updated string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `UPDATE admin SET totp_recovery_codes = ?
		WHERE id = ? AND totp_recovery_codes = ?`, updated, id, original)
	if err != nil {
		return false, fmt.Errorf("consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("consume recovery code result: %w", err)
	}
	return n == 1, nil
}

func (r *AdminRepo) ClearTOTP(ctx context.Context, id int64) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE admin SET totp_secret = '', totp_enabled = 0, totp_recovery_codes = '' WHERE id = ?`,
		id); err != nil {
		return fmt.Errorf("clear admin TOTP: %w", err)
	}
	return nil
}

func (r *AdminRepo) get(ctx context.Context, query string, arg any) (*model.Admin, error) {
	var a model.Admin
	var totpSecret, recovery sql.NullString
	var enabled int
	var createdAt int64
	err := r.db.QueryRowContext(ctx, query, arg).Scan(
		&a.ID, &a.Username, &a.PasswordHash, &totpSecret, &enabled, &recovery, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAdminNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan admin: %w", err)
	}
	a.TOTPSecret = totpSecret.String
	a.TOTPEnabled = enabled != 0
	a.TOTPRecoveryCodes = recovery.String
	a.CreatedAt = time.Unix(createdAt, 0)
	return &a, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
