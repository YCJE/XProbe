// Package config 加载 Agent 配置(/etc/probe-agent/config.yml, 权限 600, 设计文档 4.3)。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultPath 生产环境默认配置路径。
const DefaultPath = "/etc/probe-agent/config.yml"

type Config struct {
	Server                 string   `yaml:"server"`
	Token                  string   `yaml:"token"`
	RegisterCode           string   `yaml:"register_code"`
	InstallSalt            string   `yaml:"install_salt"`
	ServerCertFingerprints []string `yaml:"server_cert_fingerprints"`
	TLSInsecure            bool     `yaml:"tls_insecure"`
	StateFile              string   `yaml:"state_file"`
	ReportInterval         int      `yaml:"report_interval"`
	ConfigSyncInterval     int      `yaml:"config_sync_interval"`
	PingMethod             string   `yaml:"ping_method"`
}

// Defaults 返回带默认值的空配置。
func Defaults() *Config {
	return &Config{
		StateFile:          "/var/lib/probe-agent/state.json",
		ReportInterval:     3,
		ConfigSyncInterval: 3600,
		PingMethod:         "auto",
	}
}

// Load 读取配置文件; path 为空(开发 --once 场景)时仅返回默认值。
func Load(path string) (*Config, error) {
	c := Defaults()
	if path == "" {
		return c, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.StateFile == "" {
		c.StateFile = d.StateFile
	}
	if c.ReportInterval <= 0 {
		c.ReportInterval = d.ReportInterval
	}
	if c.ConfigSyncInterval <= 0 {
		c.ConfigSyncInterval = d.ConfigSyncInterval
	}
	if c.PingMethod == "" {
		c.PingMethod = d.PingMethod
	}
}

func (c *Config) validate() error {
	switch c.PingMethod {
	case "auto", "icmp", "tcp":
	default:
		return fmt.Errorf("config: ping_method %q not in auto/icmp/tcp", c.PingMethod)
	}
	if c.ReportInterval < 1 || c.ReportInterval > 60 {
		return fmt.Errorf("config: report_interval %d out of range [1,60]", c.ReportInterval)
	}
	return nil
}
