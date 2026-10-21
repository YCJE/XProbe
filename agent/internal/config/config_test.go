package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func valid() *Config {
	c := Defaults()
	c.Server = "http://127.0.0.1:8080"
	c.Token = "token"
	c.InstallSalt = "salt"
	c.StateFile = filepath.Join(os.TempDir(), "probe-agent-state.json")
	return &c
}

func TestHTTPSPublicServerAndURLs(t *testing.T) {
	c := valid()
	c.Server = "https://monitor.example.com"
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := c.WebSocketURL(); got != "wss://monitor.example.com/api/v1/agent/report" {
		t.Fatalf("websocket URL = %q", got)
	}
	if got := c.ConfigURL(); got != "https://monitor.example.com/api/v1/agent/config" {
		t.Fatalf("config URL = %q", got)
	}
}

func TestHTTPPublicServerIsRefused(t *testing.T) {
	c := valid()
	c.Server = "http://monitor.example.com"
	if err := c.Validate(); err == nil {
		t.Fatal("public http server was accepted")
	}
}

func TestRegistrationRequiresSalt(t *testing.T) {
	c := valid()
	c.Token = ""
	c.RegisterCode = "CODE"
	c.InstallSalt = ""
	if err := c.Validate(); err == nil {
		t.Fatal("register code without install_salt was accepted")
	}
	c.InstallSalt = "salt"
	if err := c.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestPinningRequiresHexPins(t *testing.T) {
	c := valid()
	c.TLSPinMode = "spki_sha256"
	if err := c.Validate(); err == nil {
		t.Fatal("pinning without pins was accepted")
	}
	c.ServerCertPins = []string{"AA:BB"}
	if err := c.Validate(); err == nil {
		t.Fatal("short pin was accepted")
	}
	c.ServerCertPins = []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid pin rejected: %v", err)
	}
	if c.ServerCertPins[0] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("pin was not normalized: %q", c.ServerCertPins[0])
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yml")
	c := valid()
	c.Server = "https://monitor.example.com"
	c.Token = "secret-token"
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Server != c.Server || loaded.Token != c.Token || loaded.ReportInterval != 3 {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config permissions are too broad: %s", info.Mode().Perm())
	}
}
