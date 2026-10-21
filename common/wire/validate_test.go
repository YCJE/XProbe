package wire

import (
	"errors"
	"testing"
)

func validReport() Report {
	return Report{
		Type:         TypeReport,
		Timestamp:    1718900000,
		Hostname:     "web-server-01",
		AgentVersion: "0.1.0",
		Data: ReportData{
			CPU:          CPUMetrics{Usage: 45.2, Cores: 4, Model: "Xeon", Load1: 0.5, Load5: 0.4, Load15: 0.3},
			Memory:       MemoryMetrics{Total: 8 << 30, Used: 4 << 30, SwapTotal: 2 << 30},
			Disk:         []DiskMetrics{{Device: "/", Total: 50 << 30, Used: 25 << 30}},
			Network:      NetworkMetrics{RxSpeed: 1024, TxSpeed: 512, TCPConnections: 10, UDPConnections: 2},
			TrafficMonth: TrafficMonthly{Month: "2026-09", RxBytes: 1000, TxBytes: 500},
			Uptime:       86400,
			ProcessCount: 156,
		},
	}
}

func TestReportValidateOK(t *testing.T) {
	if err := validReport().Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestReportValidateRejections(t *testing.T) {
	cases := map[string]func(*Report){
		"empty hostname":  func(r *Report) { r.Hostname = "" },
		"long hostname":   func(r *Report) { r.Hostname = string(make([]byte, MaxHostnameLen+1)) },
		"cpu over 100":    func(r *Report) { r.Data.CPU.Usage = 100.5 },
		"cpu negative":    func(r *Report) { r.Data.CPU.Usage = -1 },
		"mem used>total":  func(r *Report) { r.Data.Memory.Used = r.Data.Memory.Total + 1 },
		"swap used>total": func(r *Report) { r.Data.Memory.SwapUsed = r.Data.Memory.SwapTotal + 1 },
		"too many disks":  func(r *Report) { r.Data.Disk = make([]DiskMetrics, MaxDiskEntries+1) },
		"disk used>total": func(r *Report) { r.Data.Disk[0].Used = r.Data.Disk[0].Total + 1 },
		"negative rx":     func(r *Report) { r.Data.Network.RxSpeed = -1 },
		"negative cores":  func(r *Report) { r.Data.CPU.Cores = -1 },
		"negative uptime": func(r *Report) { r.Data.Uptime = -1 },
		"huge rx speed":   func(r *Report) { r.Data.Network.RxSpeed = MaxNetSpeedBytes + 1 },
		"negative tcp":    func(r *Report) { r.Data.Network.TCPConnections = -1 },
		"bad disk device": func(r *Report) { r.Data.Disk[0].Device = "bad\nname" },
		"bad ipv4":        func(r *Report) { r.Data.IPInfo.IPv4 = "999.1.1.1" },
		"v6 filled as v4": func(r *Report) { r.Data.IPInfo.IPv6 = "1.2.3.4" },
	}
	for name, mutate := range cases {
		r := validReport()
		mutate(&r)
		if err := r.Validate(); !errors.Is(err, ErrRejected) {
			t.Errorf("%s: expected ErrRejected, got %v", name, err)
		}
	}
}

func TestPingValidate(t *testing.T) {
	ok := PingResult{Data: []PingStat{{
		Target: "114.114.114.114", Name: "电信", Method: "icmp", IPVersion: 4,
		AvgLatency: 12.5, MinLatency: 10.2, MaxLatency: 15.8, Jitter: 1.8,
		Loss: 0, PacketsSent: 10, PacketsRecv: 10,
	}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}

	tooMany := PingResult{Data: make([]PingStat, MaxPingEntries+1)}
	if err := tooMany.Validate(); !errors.Is(err, ErrRejected) {
		t.Errorf("expected entry limit rejection, got %v", err)
	}

	badLoss := ok
	badLoss.Data[0].Loss = 101
	if err := badLoss.Validate(); !errors.Is(err, ErrRejected) {
		t.Errorf("expected loss rejection, got %v", err)
	}

	recvGtSent := ok
	recvGtSent.Data[0].PacketsRecv = 11
	if err := recvGtSent.Validate(); !errors.Is(err, ErrRejected) {
		t.Errorf("expected recv>sent rejection, got %v", err)
	}

	for name, mutate := range map[string]func(*PingStat){
		"empty target":    func(s *PingStat) { s.Target = "" },
		"bad ip version":  func(s *PingStat) { s.IPVersion = 0 },
		"negative sent":   func(s *PingStat) { s.PacketsSent = -1; s.PacketsRecv = 0 },
		"negative jitter": func(s *PingStat) { s.Jitter = -1 },
		"max too large":   func(s *PingStat) { s.MaxLatency = MaxLatencyMs + 1 },
		"control target":  func(s *PingStat) { s.Target = "1.1.1.1\n" },
	} {
		bad := ok
		bad.Data = append([]PingStat(nil), ok.Data...)
		mutate(&bad.Data[0])
		if err := bad.Validate(); !errors.Is(err, ErrRejected) {
			t.Errorf("%s: expected rejection, got %v", name, err)
		}
	}
}
