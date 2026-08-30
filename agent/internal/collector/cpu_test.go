package collector

import (
	"strings"
	"testing"
)

func statLine(user, nice, system, idle, iowait, irq, softirq, steal string) string {
	return "cpu  " + strings.Join([]string{user, nice, system, idle, iowait, irq, softirq, steal}, " ") + " 0 0 0 0\n" +
		"cpu0 50 0 50 350 0 0 0 0 0 0\ncpu1 50 0 50 350 0 0 0 0 0 0\n"
}

func TestCPUCollect_FirstSampleUsageNil(t *testing.T) {
	files := map[string][]byte{
		"/proc/stat":    []byte(statLine("100", "0", "100", "700", "0", "0", "0", "0")),
		"/proc/cpuinfo": []byte("processor\t: 0\nmodel name\t: Intel Xeon E5-2680\nprocessor\t: 1\n"),
		"/proc/loadavg": []byte("0.52 0.48 0.50 2/567 12345\n"),
	}
	c := NewCPU(newFakeSources(files, nil))

	info, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if info.Usage != nil {
		t.Fatalf("first sample Usage must be nil, got %v", *info.Usage)
	}
	if info.Cores != 2 || info.Model != "Intel Xeon E5-2680" {
		t.Fatalf("cores/model = %d/%q", info.Cores, info.Model)
	}
	if info.Load1 != 0.52 || info.Load5 != 0.48 || info.Load15 != 0.50 {
		t.Fatalf("load = %v %v %v", info.Load1, info.Load5, info.Load15)
	}
}

func TestCPUCollect_SecondSampleComputesUsage(t *testing.T) {
	files := map[string][]byte{
		"/proc/stat":    []byte(statLine("100", "0", "100", "700", "0", "0", "0", "0")),
		"/proc/cpuinfo": []byte("processor\t: 0\n"),
		"/proc/loadavg": []byte("0.10 0.10 0.10 1/1 1\n"),
	}
	c := NewCPU(newFakeSources(files, nil))
	if _, err := c.Collect(); err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	// 同一 map 被闭包引用, 原地更新即模拟时间推进
	files["/proc/stat"] = []byte(statLine("150", "0", "150", "1050", "0", "0", "0", "0"))
	info, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if info.Usage == nil {
		t.Fatal("second sample Usage must not be nil")
	}
	// idle delta 350, total delta 450 (user+50, system+50, idle+350) → usage = 1 - 350/450 ≈ 22.22
	if *info.Usage < 22.21 || *info.Usage > 22.23 {
		t.Fatalf("usage = %v, want 22.22", *info.Usage)
	}
}

func TestCPUUsage_Clamped(t *testing.T) {
	// idle 回退(异常)时 usage 会被钳制在 0-100
	prev := procStat{user: 100, idle: 500}
	cur := procStat{user: 100, idle: 100}
	if u := cpuUsage(prev, cur); u < 0 || u > 100 {
		t.Fatalf("usage %v out of range", u)
	}
}

func TestParseCPUInfo_ARMFallback(t *testing.T) {
	model, cores := parseCPUInfo([]byte("Processor\t: ARMv7 Processor rev 5\nprocessor\t: 0\nprocessor\t: 1\nprocessor\t: 2\nprocessor\t: 3\n"))
	if cores != 4 {
		t.Fatalf("cores = %d, want 4", cores)
	}
	if model != "" && !strings.Contains(model, "ARMv7") {
		t.Fatalf("unexpected model %q", model)
	}
}

func TestParseProcStat_Errors(t *testing.T) {
	if _, err := parseProcStat([]byte("intr 123\n")); err == nil {
		t.Fatal("missing cpu line should error")
	}
	if _, err := parseProcStat([]byte("cpu 1 2 3\n")); err == nil {
		t.Fatal("insufficient fields should error")
	}
}
