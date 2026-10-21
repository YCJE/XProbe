package model

import "time"

// RegisterCode is a one-time agent enrollment code. The database stores only
// the SHA-256 of the normalized code; the plaintext is shown once to the admin.
type RegisterCode struct {
	ID            int64     `json:"id"`
	CodeHash      string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Used          bool      `json:"used"`
	UsedByAgentID *int64    `json:"used_by_agent_id,omitempty"`
}
