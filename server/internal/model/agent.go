// Package model holds the database-backed domain types (design doc §5.6).
package model

import "time"

// Agent is one monitored machine. TokenHash and HostFingerprint are never
// serialized to API responses. The plaintext token exists only in the
// registration/reset response and in the agent's own config file.
type Agent struct {
	ID              int64      `json:"id"`
	TokenHash       string     `json:"-"`
	Hostname        string     `json:"hostname"`
	DisplayName     string     `json:"display_name"`
	OS              string     `json:"os"`
	Arch            string     `json:"arch"`
	AgentVersion    string     `json:"agent_version"`
	HostFingerprint string     `json:"-"`
	IPv4            string     `json:"ipv4"`
	IPv6            string     `json:"ipv6"`
	GeoLat          float64    `json:"geo_lat"`
	GeoLon          float64    `json:"geo_lon"`
	Region          string     `json:"region"`
	CountryCode     string     `json:"country_code"`
	ISP             string     `json:"isp"`
	TagIDs          string     `json:"tag_ids"` // JSON array of tag ids
	ExpiresAt       *time.Time `json:"expires_at"`
	PriceAmount     float64    `json:"price_amount"`
	PriceCurrency   string     `json:"price_currency"`
	PriceCycle      string     `json:"price_cycle"`
	TrafficQuota    int64      `json:"traffic_quota_bytes"`
	LastSeen        *time.Time `json:"last_seen"`
	Online          bool       `json:"online"`
	CreatedAt       time.Time  `json:"created_at"`
}
