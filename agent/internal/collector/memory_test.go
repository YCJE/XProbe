package collector

import "testing"

const meminfoSample = `MemTotal:       16000000 kB
MemFree:         1000000 kB
MemAvailable:    4000000 kB
Buffers:          200000 kB
Cached:          3000000 kB
SwapCached:            0 kB
SwapTotal:       2000000 kB
SwapFree:         500000 kB
HugePages_Total:       0
`

func TestParseMeminfo(t *testing.T) {
	m, err := parseMeminfo([]byte(meminfoSample))
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}
	if m.Total != 16000000*1024 {
		t.Fatalf("total = %d", m.Total)
	}
	if m.Used != (16000000-4000000)*1024 {
		t.Fatalf("used = %d", m.Used)
	}
	if m.SwapTotal != 2000000*1024 || m.SwapUsed != (2000000-500000)*1024 {
		t.Fatalf("swap = %d/%d", m.SwapTotal, m.SwapUsed)
	}
}

func TestParseMeminfo_MemAvailableFallback(t *testing.T) {
	// 内核 <3.14 无 MemAvailable: 回退 MemFree+Buffers+Cached
	s := `MemTotal:  16000000 kB
MemFree:    1000000 kB
Buffers:     200000 kB
Cached:     3000000 kB
SwapTotal:        0 kB
SwapFree:         0 kB
`
	m, err := parseMeminfo([]byte(s))
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}
	if m.Used != (16000000-4200000)*1024 {
		t.Fatalf("used = %d, want %d", m.Used, (16000000-4200000)*1024)
	}
}

func TestParseMeminfo_MissingTotal(t *testing.T) {
	if _, err := parseMeminfo([]byte("Foo: 1 kB\n")); err == nil {
		t.Fatal("missing MemTotal should error")
	}
}
