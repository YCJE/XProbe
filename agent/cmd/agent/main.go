// XProbe Agent 入口。
// M1 范围: 采集 + JSON 输出(常驻模式打印到 stdout, --once 输出一帧后退出);
// M2 接入 WebSocket 上报/注册/配置拉取。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YCJE/XProbe/agent/internal/collector"
	"github.com/YCJE/XProbe/agent/internal/config"
	"github.com/YCJE/XProbe/agent/internal/state"
	"github.com/YCJE/XProbe/internal/model"
	"github.com/YCJE/XProbe/internal/version"
)

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	configPath := flag.String("config", config.DefaultPath, "配置文件路径")
	once := flag.Bool("once", false, "采集一帧并以 JSON 输出到 stdout 后退出(验收/调试模式)")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if *once && errors.Is(err, fs.ErrNotExist) {
			// --once 开发模式允许无配置文件
			cfg = config.Defaults()
		} else {
			log.Fatalf("load config: %v", err)
		}
	}
	if !*once && cfg.Server == "" {
		log.Fatal("config: server is required (agent must know where to report)")
	}

	src := collector.DefaultSources()

	var tracker *state.Tracker
	if *once {
		tracker, err = state.Load("", nil) // 内存模式, 不落盘
	} else {
		tracker, err = state.Load(cfg.StateFile, nil)
	}
	if err != nil {
		log.Fatalf("load state: %v", err)
	}

	agent := collector.NewAgent(src, tracker)
	if h, herr := src.Hostname(); herr == nil {
		agent.SetHostname(h)
	}

	buildReport := func() model.Report {
		data, derr := agent.CollectReport()
		if derr != nil {
			log.Printf("collect: %v", derr)
		}
		return model.Report{
			Type:      model.FrameReport,
			Timestamp: time.Now().Unix(),
			Hostname:  agent.Hostname(),
			Data:      data,
		}
	}

	if *once {
		// 先采一帧初始化差值基线(CPU 首帧 Usage 为 nil), 间隔 1s 后输出真实数据帧
		_ = buildReport()
		time.Sleep(1 * time.Second)
		out, merr := json.MarshalIndent(buildReport(), "", "  ")
		if merr != nil {
			log.Fatalf("marshal report: %v", merr)
		}
		fmt.Println(string(out))
		return
	}

	// 常驻模式: 按配置间隔输出上报帧到 stdout(M2 起改走 WebSocket)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("xprobe-agent %s started: server=%s report_interval=%ds state=%s",
		version.Version, cfg.Server, cfg.ReportInterval, cfg.StateFile)

	emit := func() {
		b, merr := json.Marshal(buildReport())
		if merr != nil {
			log.Printf("marshal report: %v", merr)
			return
		}
		fmt.Println(string(b))
	}
	emit()

	ticker := time.NewTicker(time.Duration(cfg.ReportInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("shutting down")
			return
		case <-ticker.C:
			emit()
		}
	}
}
