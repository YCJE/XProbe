package model

import "time"

// Admin is the single administrator account. TOTP fields are populated in M5.
type Admin struct {
	ID                int64     `json:"id"`
	Username          string    `json:"username"`
	PasswordHash      string    `json:"-"`
	TOTPSecret        string    `json:"-"`
	TOTPEnabled       bool      `json:"totp_enabled"`
	TOTPRecoveryCodes string    `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
}
