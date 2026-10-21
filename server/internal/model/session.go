package model

import "time"

// Session is one admin login session. The cookie carries a random token; the
// database stores only its SHA-256 so a database or backup leak cannot be
// replayed. ID is a separate random identifier for listing and revocation.
type Session struct {
	ID            string     `json:"id"`
	TokenHash     string     `json:"-"`
	AdminID       int64      `json:"admin_id"`
	ExpiresAt     time.Time  `json:"expires_at"`
	IdleExpiresAt time.Time  `json:"idle_expires_at"`
	Revoked       bool       `json:"revoked"`
	CreatedAt     time.Time  `json:"created_at"`
	LastSeenAt    *time.Time `json:"last_seen_at"`
	IP            string     `json:"ip"`
	UserAgent     string     `json:"user_agent"`
}
