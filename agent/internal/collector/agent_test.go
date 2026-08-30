package collector

import (
	"testing"

	"github.com/YCJE/XProbe/internal/model"
)

type fakeTracker struct {
	calls int
	last  model.TrafficMonthly
}

func (f *fakeTracker) Update(rx, tx uint64) model.TrafficMonthly {
	f.calls++
	f.last = model.TrafficMonthly{Month: "2026-08", RxBytes: rx, TxBytes: tx}
	return f.last
}

func TestAgentCollectReport_TwoSamples(t *testing.T) {
	files := map[string][]byte{
		"/proc/stat":      []byte(statLine("100", "0", "100", "700", "0", "0", "0", "0")),
		"/proc/cpuinfo":   []byte("processor\t: 0\nmodel name\t: Test CPU\n"),
		"/proc/loadavg":   []byte("0.10 0.20 0.30 1/1 1\n"),
		"/proc/meminfo":   []byte("MemTotal: 16000000 kB\nMemAvailable: 4000000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n"),
		"/proc/mounts":    []byte("/dev/sda1 / ext4 rw 0 0\n"),
		"/proc/net/dev":   []byte(netdevSample),
		"/proc/net/tcp":   []byte(procNetTCPSample),
		"/proc/net/udp":   []byte(procNetUDPSample),
		"/proc/uptime":    []byte("86400.0 0.0\n"),
		"/etc/os-release": []byte("PRETTY_NAME=\"TestOS 1.0\"\n"),
	}
	dirs := map[string][]string{"/proc": {"1", "2", "self"}}

	agg := &fakeTracker{}
	src := newFakeSources(files, dirs)
	src.Statfs = func(string) (FsStat, error) { return FsStat{Blocks: 1000, Bavail: 250, Bsize: 4096}, nil }
	a := NewAgent(src, agg)
	a.SetHostname("web-01")

	d1, err := a.CollectReport()
	if err != nil {
		t.Fatalf("first CollectReport: %v", err)
	}
	if d1.CPU.Usage != nil {
		t.Fatal("first report cpu.Usage must be nil")
	}
	if d1.Memory.Total != 16000000*1024 {
		t.Fatalf("memory total = %d", d1.Memory.Total)
	}
	if len(d1.Disk) != 1 || d1.Disk[0].Device != "/" {
		t.Fatalf("disk = %+v", d1.Disk)
	}
	if d1.ProcessCount != 2 {
		t.Fatalf("process count = %d", d1.ProcessCount)
	}
	if d1.TrafficMonthly.RxBytes == 0 {
		t.Fatal("traffic should be updated from net totals")
	}
	if agg.calls != 1 {
		t.Fatalf("tracker calls = %d", agg.calls)
	}

	// 第二帧: CPU 产生真实差值(闭包读同一 map, 原地更新即可)
	files["/proc/stat"] = []byte(statLine("150", "0", "150", "1050", "0", "0", "0", "0"))
	d2, err := a.CollectReport()
	if err != nil {
		t.Fatalf("second CollectReport: %v", err)
	}
	if d2.CPU.Usage == nil || *d2.CPU.Usage < 22.21 || *d2.CPU.Usage > 22.23 {
		t.Fatalf("second usage = %v, want 22.22", d2.CPU.Usage)
	}
	if d2.System.OS != "TestOS 1.0" {
		t.Fatalf("system os = %q", d2.System.OS)
	}
}
