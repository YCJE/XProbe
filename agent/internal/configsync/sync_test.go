package configsync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/YCJE/XProbe/internal/model"
)

func TestPull_AuthHeaderAndPayload(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ping_targets":[{"target":"114.114.114.114","name":"电信","isp":"ct","ip_version":4,"protocol":"icmp"}]}`))
	}))
	defer srv.Close()

	p, err := Pull(context.Background(), srv.Client(), srv.URL, "tok-1")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if gotAuth != "Bearer tok-1" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if len(p.PingTargets) != 1 || p.PingTargets[0].Name != "电信" || p.PingTargets[0].IPVersion != 4 {
		t.Fatalf("payload = %+v", p)
	}
}

func TestPull_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := Pull(context.Background(), srv.Client(), srv.URL, "bad"); err == nil {
		t.Fatal("401 should error")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "targets.json")
	src := &model.AgentConfigPayload{PingTargets: []model.PingTarget{
		{Target: "223.5.5.5", Name: "阿里", IPVersion: 4, Protocol: "icmp"},
	}}
	if err := SaveCache(path, src); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	if runtime.GOOS != "windows" { // Windows 不映射 POSIX 权限位; S8 以生产 Linux 为准
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("cache perms wrong: %v", err)
		}
	}
	got, err := LoadCache(path)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if len(got.PingTargets) != 1 || got.PingTargets[0].Target != "223.5.5.5" {
		t.Fatalf("loaded = %+v", got)
	}
}
