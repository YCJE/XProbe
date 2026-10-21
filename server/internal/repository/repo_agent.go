package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"probe/server/internal/model"
)

// AgentRepo provides raw-SQL persistence for agents (§5.6).
type AgentRepo struct {
	db *sql.DB
}

func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

// ErrAgentNotFound is returned when no row matches.
var ErrAgentNotFound = errors.New("agent not found")

// ErrRegisterCodeInvalid covers missing, used and expired codes. The API must
// return the same answer for all three so it does not become an oracle.
var ErrRegisterCodeInvalid = errors.New("register code invalid")

// ErrFingerprintConflict means another agent already owns this fingerprint.
var ErrFingerprintConflict = errors.New("host fingerprint already registered")

func (r *AgentRepo) Create(ctx context.Context, a *model.Agent) (int64, error) {
	if a.TagIDs == "" {
		a.TagIDs = "[]"
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO agents
		(token_hash, hostname, display_name, os, arch, agent_version, host_fingerprint,
		 ipv4, ipv6, tag_ids, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TokenHash, a.Hostname, a.DisplayName, a.OS, a.Arch, a.AgentVersion,
		a.HostFingerprint, a.IPv4, a.IPv6, a.TagIDs, time.Now().Unix())
	if err != nil {
		return 0, fmt.Errorf("insert agent: %w", err)
	}
	return res.LastInsertId()
}

// Register consumes a one-time registration code and inserts the agent in a
// single transaction. A code is never spent if the insert fails, and a
// fingerprint conflict never creates a second row.
func (r *AgentRepo) Register(ctx context.Context, codeHash string, a *model.Agent, now time.Time) (int64, error) {
	if codeHash == "" || a.TokenHash == "" || a.Hostname == "" || a.HostFingerprint == "" {
		return 0, fmt.Errorf("register agent: code hash, token hash, hostname and fingerprint are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin register: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var codeID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM register_codes
		WHERE code_hash = ? AND used = 0 AND expires_at > ?`, codeHash, now.Unix()).Scan(&codeID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRegisterCodeInvalid
	}
	if err != nil {
		return 0, fmt.Errorf("lookup register code: %w", err)
	}

	var existing int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE host_fingerprint = ?`,
		a.HostFingerprint).Scan(&existing)
	if err == nil {
		return 0, ErrFingerprintConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("lookup fingerprint: %w", err)
	}

	tagIDs := a.TagIDs
	if tagIDs == "" {
		tagIDs = "[]"
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO agents
		(token_hash, hostname, display_name, os, arch, agent_version, host_fingerprint,
		 ipv4, ipv6, tag_ids, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.TokenHash, a.Hostname, a.DisplayName, a.OS, a.Arch, a.AgentVersion,
		a.HostFingerprint, a.IPv4, a.IPv6, tagIDs, now.Unix())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: agents.host_fingerprint") {
			return 0, ErrFingerprintConflict
		}
		return 0, fmt.Errorf("insert registered agent: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("registered agent id: %w", err)
	}

	res, err = tx.ExecContext(ctx, `UPDATE register_codes
		SET used = 1, used_by_agent_id = ? WHERE id = ? AND used = 0`, id, codeID)
	if err != nil {
		return 0, fmt.Errorf("consume register code: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return 0, ErrRegisterCodeInvalid
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit register: %w", err)
	}
	a.ID = id
	a.CreatedAt = now
	return id, nil
}

func (r *AgentRepo) GetByID(ctx context.Context, id int64) (*model.Agent, error) {
	return r.get(ctx, `SELECT `+agentCols+` FROM agents WHERE id = ?`, id)
}

// GetByTokenHash looks up an agent by SHA-256(token). Callers hash the
// presented token in constant time and never persist the plaintext.
func (r *AgentRepo) GetByTokenHash(ctx context.Context, tokenHash string) (*model.Agent, error) {
	return r.get(ctx, `SELECT `+agentCols+` FROM agents WHERE token_hash = ?`, tokenHash)
}

func (r *AgentRepo) List(ctx context.Context) ([]model.Agent, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+agentCols+` FROM agents ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var out []model.Agent
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// UpdateMeta sets the admin-managed metadata fields (§5.8). Agent-reported
// fields (token, hostname, fingerprint) are intentionally untouched.
func (r *AgentRepo) UpdateMeta(ctx context.Context, a *model.Agent) error {
	res, err := r.db.ExecContext(ctx, `UPDATE agents SET
		display_name = ?, region = ?, country_code = ?, isp = ?, tag_ids = ?,
		expires_at = ?, price_amount = ?, price_currency = ?, price_cycle = ?,
		traffic_quota_bytes = ?, geo_lat = ?, geo_lon = ?
		WHERE id = ?`,
		a.DisplayName, a.Region, a.CountryCode, a.ISP, a.TagIDs,
		nullTime(a.ExpiresAt), a.PriceAmount, a.PriceCurrency, a.PriceCycle,
		a.TrafficQuota, a.GeoLat, a.GeoLon, a.ID)
	if err != nil {
		return fmt.Errorf("update agent meta: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

func (r *AgentRepo) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAgentNotFound
	}
	return nil
}

// ResetOnline clears stale online flags at process start; every agent must
// reconnect before it is considered live again.
func (r *AgentRepo) ResetOnline(ctx context.Context) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE agents SET online = 0`); err != nil {
		return fmt.Errorf("reset online state: %w", err)
	}
	return nil
}

// SetOnline flips the online flag and stamps last_seen (§5.3 offline detection).
func (r *AgentRepo) SetOnline(ctx context.Context, id int64, online bool, at time.Time) error {
	onlineInt := 0
	if online {
		onlineInt = 1
	}
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agents SET online = ?, last_seen = ? WHERE id = ?`,
		onlineInt, at.Unix(), id); err != nil {
		return fmt.Errorf("set agent online: %w", err)
	}
	return nil
}

// UpdateIPs persists addresses reported by the agent. Empty values are
// ignored so a transient collection failure does not erase the last known IP.
func (r *AgentRepo) UpdateIPs(ctx context.Context, id int64, ipv4, ipv6 string) error {
	if _, err := r.db.ExecContext(ctx, `UPDATE agents SET
		ipv4 = CASE WHEN ? <> '' THEN ? ELSE ipv4 END,
		ipv6 = CASE WHEN ? <> '' THEN ? ELSE ipv6 END
		WHERE id = ?`, ipv4, ipv4, ipv6, ipv6, id); err != nil {
		return fmt.Errorf("update agent IPs: %w", err)
	}
	return nil
}

// Touch updates last_seen and agent_version (from report frames, §4.4).
func (r *AgentRepo) Touch(ctx context.Context, id int64, agentVersion string, at time.Time) error {
	if _, err := r.db.ExecContext(ctx,
		`UPDATE agents SET last_seen = ?, agent_version = ? WHERE id = ?`,
		at.Unix(), agentVersion, id); err != nil {
		return fmt.Errorf("touch agent: %w", err)
	}
	return nil
}

const agentCols = `id, token_hash, hostname, display_name, os, arch, agent_version,
	host_fingerprint, ipv4, ipv6, geo_lat, geo_lon, region, country_code, isp,
	tag_ids, expires_at, price_amount, price_currency, price_cycle,
	traffic_quota_bytes, last_seen, online, created_at`

func (r *AgentRepo) get(ctx context.Context, query string, args ...any) (*model.Agent, error) {
	row := r.db.QueryRowContext(ctx, query, args...)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrAgentNotFound
	}
	return a, err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanAgent(row rowScanner) (*model.Agent, error) {
	var a model.Agent
	var displayName, osName, arch, agentVersion, fingerprint sql.NullString
	var ipv4, ipv6, region, country, isp sql.NullString
	var priceCurrency, priceCycle sql.NullString
	var geoLat, geoLon sql.NullFloat64
	var priceAmount sql.NullFloat64
	var expiresAt, lastSeen sql.NullInt64
	var createdAt int64

	err := row.Scan(&a.ID, &a.TokenHash, &a.Hostname, &displayName, &osName, &arch,
		&agentVersion, &fingerprint, &ipv4, &ipv6, &geoLat, &geoLon,
		&region, &country, &isp, &a.TagIDs, &expiresAt, &priceAmount,
		&priceCurrency, &priceCycle, &a.TrafficQuota, &lastSeen, &a.Online, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan agent: %w", err)
	}
	a.DisplayName = displayName.String
	a.OS = osName.String
	a.Arch = arch.String
	a.AgentVersion = agentVersion.String
	a.HostFingerprint = fingerprint.String
	a.IPv4 = ipv4.String
	a.IPv6 = ipv6.String
	a.Region = region.String
	a.CountryCode = country.String
	a.ISP = isp.String
	a.PriceCurrency = priceCurrency.String
	a.PriceCycle = priceCycle.String
	a.GeoLat = geoLat.Float64
	a.GeoLon = geoLon.Float64
	a.PriceAmount = priceAmount.Float64
	if expiresAt.Valid {
		t := time.Unix(expiresAt.Int64, 0)
		a.ExpiresAt = &t
	}
	if lastSeen.Valid {
		t := time.Unix(lastSeen.Int64, 0)
		a.LastSeen = &t
	}
	a.CreatedAt = time.Unix(createdAt, 0)
	return &a, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Unix()
}
