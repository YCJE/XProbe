package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yml")); !os.IsNotExist(err) {
		t.Fatalf("err = %v, want not exist", err)
	}
}

func TestLoad_EmptyPathDefaults(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ReportInterval != 3 || c.StateFile != "/var/lib/probe-agent/state.json" || c.PingMethod != "auto" {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadAndSave_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	src := `server: "https://probe.example.com"
token: "tok-abc"
install_salt: "salt-xyz"
server_cert_fingerprints:
  - "aa11"
  - "bb22"
report_interval: 5
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Server != "https://probe.example.com" || c.Token != "tok-abc" || len(c.ServerCertFingerprints) != 2 {
		t.Fatalf("parsed = %+v", c)
	}

	// 注册成功后: 写回 Token, 清除注册码
	c.Token = "tok-new"
	c.RegisterCode = ""
	if err := Save(path, c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	c2, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if c2.Token != "tok-new" || c2.RegisterCode != "" || c2.InstallSalt != "salt-xyz" {
		t.Fatalf("after save = %+v", c2)
	}

	// 权限 600(S8); Windows 不映射 POSIX 权限位, 以生产 Linux 为准
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("config perms = %v, want 600", info.Mode().Perm())
		}
	}
}

func TestLoad_InvalidPingMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	os.WriteFile(path, []byte("ping_method: \"dns\"\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("invalid ping_method should error")
	}
}
