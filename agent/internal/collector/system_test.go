package collector

import (
	"testing"

	"github.com/YCJE/XProbe/internal/version"
)

func TestSystemCollect(t *testing.T) {
	files := map[string][]byte{
		"/proc/uptime":    []byte("86400.25 123456.00\n"),
		"/etc/os-release": []byte("NAME=\"Ubuntu\"\nPRETTY_NAME=\"Ubuntu 22.04.5 LTS\"\n"),
	}
	dirs := map[string][]string{
		"/proc": {"1", "42", "123", "self", "cpuinfo", "uptime", "acpi"},
	}
	src := newFakeSources(files, dirs)

	uptime, info, procCount, err := NewSystem(src).Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if uptime != 86400 {
		t.Fatalf("uptime = %d", uptime)
	}
	if info.OS != "Ubuntu 22.04.5 LTS" {
		t.Fatalf("os = %q", info.OS)
	}
	if info.Kernel != "5.15.0-generic" {
		t.Fatalf("kernel = %q", info.Kernel)
	}
	if info.Arch == "" || info.AgentVersion != version.Version {
		t.Fatalf("arch/version = %q/%q", info.Arch, info.AgentVersion)
	}
	// 只统计数字目录(进程), 忽略 self/acpi 等
	if procCount != 3 {
		t.Fatalf("procCount = %d, want 3", procCount)
	}
}

func TestParseUptime_Errors(t *testing.T) {
	if _, err := parseUptime([]byte("\n")); err == nil {
		t.Fatal("empty input should error")
	}
	if _, err := parseUptime([]byte("not-a-number 1\n")); err == nil {
		t.Fatal("non-numeric should error")
	}
}
