package model

import "time"

// Alert status values used by the alert engine state machine (§5.4).
const (
	AlertPending  = "PENDING"
	AlertFiring   = "FIRING"
	AlertResolved = "RESOLVED"
)

// AlertScope restricts which agents a rule applies to (§5.4, v1.3).
type AlertScope struct {
	Type string  `json:"type"` // "all" | "agent_ids" | "tag_ids"
	IDs  []int64 `json:"ids,omitempty"`
}

// AlertRule is one threshold rule; a rule can notify multiple channels.
type AlertRule struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Metric           string     `json:"metric"`
	Operator         string     `json:"operator"` // ">" | "<" | "="
	Threshold        float64    `json:"threshold"`
	DurationSec      int        `json:"duration"`
	Enabled          bool       `json:"enabled"`
	Scope            AlertScope `json:"scope"`
	NotifyChannelIDs []int64    `json:"notify_channel_ids"`
	CreatedAt        time.Time  `json:"created_at"`
}

// AlertHistory persists the state machine so restarts do not re-notify (§5.4).
type AlertHistory struct {
	ID        int64     `json:"id"`
	RuleID    int64     `json:"rule_id"`
	AgentID   int64     `json:"agent_id"`
	Status    string    `json:"status"`
	Value     float64   `json:"value"`
	StartedAt time.Time `json:"started_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Notified  bool      `json:"notified"`
}
