// Package config loads the agent's YAML configuration and derives the REST /
// WebSocket URLs. The config file contains the agent token, so Save always
// writes it with mode 0600.
package config

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server             string   `yaml:"server"`
	Token              string   `yaml:"token,omitempty"`
	RegisterCode       string   `yaml:"register_code,omitempty"`
	InstallSalt        string   `yaml:"install_salt,omitempty"`
	TLSPinMode         string   `yaml:"tls_pin_mode,omitempty"`
	ServerCertPins     []string `yaml:"server_cert_fingerprints,omitempty"`
	TLSInsecure        bool     `yaml:"tls_insecure,omitempty"`
	StateFile          string   `yaml:"state_file"`
	ReportInterval     int      `yaml:"report_interval"`
	ConfigSyncInterval int      `yaml:"config_sync_interval"`
	PingMethod         string   `yaml:"ping_method"`
}

func Defaults() Config {
	state := "/var/lib/probe-agent/state.json"
	if runtime.GOOS == "windows" {
		state = "probe-agent.state.json"
	}
	return Config{
		TLSPinMode:         "none",
		StateFile:          state,
		ReportInterval:     3,
		ConfigSyncInterval: 3600,
		PingMethod:         "auto",
	}
}

func Load(path string) (*Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Validate() error {
	u, err := url.Parse(c.Server)
	if err != nil || u.Host == "" {
		return fmt.Errorf("server must be an absolute http(s) URL")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("server must not contain a query or fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopbackHost(u.Hostname()) {
			return fmt.Errorf("server over http is only allowed for a loopback host")
		}
	default:
		return fmt.Errorf("server must use http or https")
	}
	if c.Token == "" && c.RegisterCode == "" {
		return fmt.Errorf("token or register_code is required")
	}
	if c.RegisterCode != "" && c.InstallSalt == "" {
		return fmt.Errorf("install_salt is required while using a register_code")
	}
	if c.TLSInsecure && !loopbackHost(u.Hostname()) {
		return fmt.Errorf("tls_insecure is only allowed for a loopback server")
	}
	switch c.TLSPinMode {
	case "", "none":
		c.TLSPinMode = "none"
	case "spki_sha256":
		if len(c.ServerCertPins) == 0 {
			return fmt.Errorf("tls_pin_mode=spki_sha256 requires server_cert_fingerprints")
		}
		normalized := make([]string, 0, len(c.ServerCertPins))
		for _, pin := range c.ServerCertPins {
			raw := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(pin), ":", ""))
			if b, err := hex.DecodeString(raw); err != nil || len(b) != 32 {
				return fmt.Errorf("invalid SPKI pin %q", pin)
			}
			normalized = append(normalized, raw)
		}
		c.ServerCertPins = normalized
	default:
		return fmt.Errorf("tls_pin_mode must be none or spki_sha256")
	}
	if c.TLSInsecure && c.TLSPinMode != "none" {
		return fmt.Errorf("tls_insecure cannot be combined with tls_pin_mode")
	}
	if c.ReportInterval < 1 || c.ReportInterval > 60 {
		return fmt.Errorf("report_interval must be between 1 and 60 seconds")
	}
	if c.ConfigSyncInterval < 60 || c.ConfigSyncInterval > 86400 {
		return fmt.Errorf("config_sync_interval must be between 60 and 86400 seconds")
	}
	switch c.PingMethod {
	case "auto", "icmp", "tcp":
	default:
		return fmt.Errorf("ping_method must be auto, icmp or tcp")
	}
	if strings.TrimSpace(c.StateFile) == "" {
		return fmt.Errorf("state_file must not be empty")
	}
	return nil
}

func (c *Config) WebSocketURL() string {
	return strings.TrimRight(httpToWS(c.Server), "/") + "/api/v1/agent/report"
}

func (c *Config) ConfigURL() string {
	return strings.TrimRight(c.Server, "/") + "/api/v1/agent/config"
}

func (c *Config) RegisterURL() string {
	return strings.TrimRight(c.Server, "/") + "/api/v1/agent/register"
}

// Save writes the config atomically with 0600 permissions.
func (c *Config) Save(path string) error {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func httpToWS(raw string) string {
	return strings.NewReplacer("https://", "wss://", "http://", "ws://").Replace(raw)
}

func loopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
