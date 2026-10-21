package model

import "time"

// NotifyChannel is one outbound notification target (webhook/telegram/smtp).
// Config holds the channel-specific JSON; secrets inside are masked on API
// responses (§5.5).
type NotifyChannel struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Config    string    `json:"config"`
	CreatedAt time.Time `json:"created_at"`
}

// PingTarget is one probe destination pushed to agents via config sync (§4.7).
type PingTarget struct {
	ID        int64     `json:"id"`
	Target    string    `json:"target"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	ISP       string    `json:"isp"`
	IPVersion int       `json:"ip_version"`
	Protocol  string    `json:"protocol"` // icmp | tcp | http
	IsDefault bool      `json:"is_default"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}
