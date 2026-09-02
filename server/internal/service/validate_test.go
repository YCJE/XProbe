package service

import (
	"testing"
	"time"

	"github.com/YCJE/XProbe/internal/model"
)

func validReport() *model.Report {
	u := 42.5
	return &model.Report{
		Type:      model.FrameReport,
		Timestamp: time.Now().Unix(), // 服务端 ±300s 窗口内(审查 MEDIUM #5)
		Hostname:  "web-01",
		Data: model.ReportData{
			CPU:            model.CPUInfo{Usage: &u, Cores: 4},
			Memory:         model.MemoryInfo{Total: 1000, Used: 500},
			Disk:           []model.DiskUsage{{Device: "/", Total: 100, Used: 70}},
			TrafficMonthly: model.TrafficMonthly{Month: "2026-08", RxBytes: 1, TxBytes: 1},
		},
	}
}

func TestValidateReport_OK(t *testing.T) {
	if err := ValidateReport(validReport(), time.Now().Unix()); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
}

func TestValidateReport_Rejections(t *testing.T) {
	cases := map[string]func(*model.Report){
		"cpu>100":    func(r *model.Report) { v := 100.5; r.Data.CPU.Usage = &v },
		"cpu<0":      func(r *model.Report) { v := -1.0; r.Data.CPU.Usage = &v },
		"mem>total":  func(r *model.Report) { r.Data.Memory.Used = 2000 },
		"swap>total": func(r *model.Report) { r.Data.Memory.SwapTotal = 100; r.Data.Memory.SwapUsed = 200 },
		"disk>total": func(r *model.Report) { r.Data.Disk[0].Used = 200 },
		"bad month":  func(r *model.Report) { r.Data.TrafficMonthly.Month = "2026-8" },
		"no host":    func(r *model.Report) { r.Hostname = "" },
	}
	for name, mutate := range cases {
		r := validReport()
		mutate(r)
		if err := ValidateReport(r, time.Now().Unix()); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestValidateReport_NilUsageAllowed(t *testing.T) {
	r := validReport()
	r.Data.CPU.Usage = nil // 首采样为空是合法状态(设计文档 4.1)
	if err := ValidateReport(r, time.Now().Unix()); err != nil {
		t.Fatalf("nil usage should pass: %v", err)
	}
}

func TestValidatePingResults(t *testing.T) {
	ok := []model.PingResult{{Target: "114.114.114.114", Name: "电信", IPVersion: 4,
		AvgLatency: 12.5, MinLatency: 10, MaxLatency: 15, Loss: 0, PacketsSent: 10, PacketsRecv: 10}}
	if err := ValidatePingResults(ok); err != nil {
		t.Fatalf("valid ping rejected: %v", err)
	}
	bad := []model.PingResult{ok[0]}
	bad[0].AvgLatency = 60001
	if err := ValidatePingResults(bad); err == nil {
		t.Fatal("latency > 60000 should reject")
	}
	bad = []model.PingResult{ok[0]}
	bad[0].Loss = 101
	if err := ValidatePingResults(bad); err == nil {
		t.Fatal("loss > 100 should reject")
	}
	bad = []model.PingResult{ok[0]}
	bad[0].PacketsRecv = 11
	if err := ValidatePingResults(bad); err == nil {
		t.Fatal("recv > sent should reject")
	}
	bad = []model.PingResult{ok[0]}
	bad[0].IPVersion = 5
	if err := ValidatePingResults(bad); err == nil {
		t.Fatal("ip_version 5 should reject")
	}
}
