// Package config loads and validates the server configuration.
//
// The defaults are deliberately safe for local use: bind to loopback, no TLS
// (only permitted on loopback), no trusted proxy. Public deployments must opt
// into files-based TLS or a Cloudflare entry mode.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("parse duration %q: %w", raw, err)
	}
	d.Duration = parsed
	return nil
}

func (d Duration) MarshalYAML() (any, error) {
	return d.Duration.String(), nil
}

type Config struct {
	Listen    string        `yaml:"listen"`
	PublicURL string        `yaml:"public_url"`
	AdminURL  string        `yaml:"admin_url"`
	DataDir   string        `yaml:"data_dir"`
	TLS       TLSConfig     `yaml:"tls"`
	Proxy     ProxyConfig   `yaml:"proxy"`
	Auth      AuthConfig    `yaml:"auth"`
	Monitor   MonitorConfig `yaml:"monitor"`
	Storage   StorageConfig `yaml:"storage"`
	Log       LogConfig     `yaml:"log"`
}

type TLSConfig struct {
	Mode string `yaml:"mode"` // files|off
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type ProxyConfig struct {
	Mode         string   `yaml:"mode"` // none|cloudflare_tunnel|cloudflare_proxy
	TrustedCIDRs []string `yaml:"trusted_cidrs"`
}

type AuthConfig struct {
	SessionIdle    Duration `yaml:"session_idle"`
	SessionMax     Duration `yaml:"session_max"`
	LoginRateLimit int      `yaml:"login_rate_limit"`
}

type MonitorConfig struct {
	ReportInterval    int `yaml:"report_interval"`
	HeartbeatTimeout  int `yaml:"heartbeat_timeout"`
	RingBufferSize    int `yaml:"ring_buffer_size"`
	AggregationSecond int `yaml:"aggregation_interval"`
}

type StorageConfig struct {
	DataRetentionDays int `yaml:"data_retention_days"`
	BusyTimeoutMS     int `yaml:"sqlite_busy_timeout_ms"`
}

type LogConfig struct {
	Level string `yaml:"level"`
}

// Defaults returns the local-development baseline. It is safe to bind to
// loopback with TLS off; any public listener must set tls.mode=files.
func Defaults() Config {
	return Config{
		Listen:  "127.0.0.1:8080",
		DataDir: "./data",
		TLS:     TLSConfig{Mode: "off"},
		Proxy:   ProxyConfig{Mode: "none"},
		Auth: AuthConfig{
			SessionIdle:    Duration{2 * time.Hour},
			SessionMax:     Duration{168 * time.Hour},
			LoginRateLimit: 5,
		},
		Monitor: MonitorConfig{
			ReportInterval:    3,
			HeartbeatTimeout:  90,
			RingBufferSize:    3600,
			AggregationSecond: 300,
		},
		Storage: StorageConfig{
			DataRetentionDays: 90,
			BusyTimeoutMS:     5000,
		},
		Log: LogConfig{Level: "info"},
	}
}

// Load reads path (when non-empty) over the defaults and validates the result.
// An explicitly supplied path that does not exist is an error; an empty path
// means "defaults only".
func Load(path string) (*Config, error) {
	cfg := Defaults()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.DataDir) == "" {
		return fmt.Errorf("data_dir must not be empty")
	}
	if _, _, err := net.SplitHostPort(c.Listen); err != nil {
		return fmt.Errorf("listen %q must be host:port: %w", c.Listen, err)
	}
	loopback := loopbackListen(c.Listen)

	switch c.TLS.Mode {
	case "files":
		if c.TLS.Cert == "" || c.TLS.Key == "" {
			return fmt.Errorf("tls.cert and tls.key are required when tls.mode=files")
		}
	case "off":
		if !loopback {
			return fmt.Errorf("tls.mode=off is only allowed on a loopback listener; use tls.mode=files or Cloudflare Tunnel")
		}
	default:
		return fmt.Errorf("tls.mode must be files or off")
	}

	switch c.Proxy.Mode {
	case "none":
	case "cloudflare_tunnel":
		if !loopback || c.TLS.Mode != "off" {
			return fmt.Errorf("cloudflare_tunnel requires a loopback listener with tls.mode=off")
		}
	case "cloudflare_proxy":
		if c.TLS.Mode != "files" {
			return fmt.Errorf("cloudflare_proxy requires tls.mode=files so the origin is HTTPS")
		}
		if len(c.Proxy.TrustedCIDRs) == 0 {
			return fmt.Errorf("cloudflare_proxy requires proxy.trusted_cidrs")
		}
		for _, cidr := range c.Proxy.TrustedCIDRs {
			if _, _, err := net.ParseCIDR(cidr); err != nil {
				return fmt.Errorf("invalid proxy trusted CIDR %q: %w", cidr, err)
			}
		}
	default:
		return fmt.Errorf("proxy.mode must be none, cloudflare_tunnel or cloudflare_proxy")
	}

	if c.PublicURL == "" {
		if !loopback {
			return fmt.Errorf("public_url is required for a non-loopback listener")
		}
		c.PublicURL = "http://" + c.Listen
	}
	if err := validateURL("public_url", c.PublicURL); err != nil {
		return err
	}
	if !loopback {
		if u, err := url.Parse(c.PublicURL); err == nil && loopbackHost(u.Hostname()) {
			return fmt.Errorf("public_url must not be a loopback address for a public listener")
		}
	}
	if c.AdminURL != "" {
		if err := validateURL("admin_url", c.AdminURL); err != nil {
			return err
		}
		if !loopback {
			if u, err := url.Parse(c.AdminURL); err == nil && loopbackHost(u.Hostname()) {
				return fmt.Errorf("admin_url must not be a loopback address for a public listener")
			}
		}
	}

	if c.Auth.SessionIdle.Duration <= 0 || c.Auth.SessionMax.Duration <= 0 {
		return fmt.Errorf("auth.session_idle and auth.session_max must be positive")
	}
	if c.Auth.SessionMax.Duration < c.Auth.SessionIdle.Duration {
		return fmt.Errorf("auth.session_max must be >= auth.session_idle")
	}
	if c.Auth.LoginRateLimit <= 0 {
		return fmt.Errorf("auth.login_rate_limit must be positive")
	}
	if c.Monitor.ReportInterval <= 0 || c.Monitor.HeartbeatTimeout <= 0 {
		return fmt.Errorf("monitor.report_interval and monitor.heartbeat_timeout must be positive")
	}
	if c.Storage.DataRetentionDays < 0 {
		return fmt.Errorf("storage.data_retention_days must not be negative")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be debug, info, warn or error")
	}
	return nil
}

func validateURL(name, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s must be an absolute URL: %q", name, raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must not contain a query or fragment: %q", name, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if !loopbackHost(u.Hostname()) {
			return fmt.Errorf("%s over http is only allowed on a loopback host", name)
		}
		return nil
	default:
		return fmt.Errorf("%s must use http or https: %q", name, raw)
	}
}

func loopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	return loopbackHost(host)
}

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
