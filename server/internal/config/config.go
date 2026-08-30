// Package config 加载 Server 配置(设计文档 8.2)。
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultPath 生产环境默认配置路径。
const DefaultPath = "/etc/probe-server/config.yml"

type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type AuthConfig struct {
	JWTSecret    string `yaml:"jwt_secret"`     // 留空则 env 或首次生成持久化(设计文档 8.2)
	CookieSecure *bool  `yaml:"cookie_secure"`  // 默认 true; 仅本地 http 开发设 false
	JWTExpiryMin int    `yaml:"jwt_expiry_min"` // 默认 120(2h, 设计文档 7.3)
}

type MonitorConfig struct {
	HeartbeatTimeout int   `yaml:"heartbeat_timeout"` // 秒, 默认 90
	WSCompression    *bool `yaml:"ws_compression"`    // 默认 true
}

type SecurityConfig struct {
	RegisterRateLimit int `yaml:"register_rate_limit"` // 次/分钟/IP, 默认 5
	GlobalRateLimit   int `yaml:"global_rate_limit"`   // 次/分钟/IP, 默认 120
}

type Config struct {
	Listen   string         `yaml:"listen"`
	DataDir  string         `yaml:"data_dir"`
	TLS      TLSConfig      `yaml:"tls"`
	Auth     AuthConfig     `yaml:"auth"`
	Monitor  MonitorConfig  `yaml:"monitor"`
	Security SecurityConfig `yaml:"security"`
}

// Defaults 返回带默认值的空配置。
func Defaults() *Config {
	compress := true
	secure := true
	c := &Config{
		Listen:  ":443",
		DataDir: "/var/lib/probe-server",
	}
	c.Auth.CookieSecure = &secure
	c.Auth.JWTExpiryMin = 120
	c.Monitor.HeartbeatTimeout = 90
	c.Monitor.WSCompression = &compress
	c.Security.RegisterRateLimit = 5
	c.Security.GlobalRateLimit = 120
	return c
}

// Load 读取配置文件; path 为空时仅返回默认值(开发模式)。
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
	return c, nil
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.Listen == "" {
		c.Listen = d.Listen
	}
	if c.DataDir == "" {
		c.DataDir = d.DataDir
	}
	if c.Auth.CookieSecure == nil {
		c.Auth.CookieSecure = d.Auth.CookieSecure
	}
	if c.Auth.JWTExpiryMin <= 0 {
		c.Auth.JWTExpiryMin = d.Auth.JWTExpiryMin
	}
	if c.Monitor.HeartbeatTimeout <= 0 {
		c.Monitor.HeartbeatTimeout = d.Monitor.HeartbeatTimeout
	}
	if c.Monitor.WSCompression == nil {
		c.Monitor.WSCompression = d.Monitor.WSCompression
	}
	if c.Security.RegisterRateLimit <= 0 {
		c.Security.RegisterRateLimit = d.Security.RegisterRateLimit
	}
	if c.Security.GlobalRateLimit <= 0 {
		c.Security.GlobalRateLimit = d.Security.GlobalRateLimit
	}
}
