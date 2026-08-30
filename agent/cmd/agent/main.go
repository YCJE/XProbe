// XProbe Agent 入口。
// M2 流程: 校验 https(S2) → 无 Token 则 REST 注册换 Token 并落盘(S9/S8) →
// WebSocket 上报(退避重连+jitter/心跳/证书 Pinning) + 定时配置拉取(只读)。
// --once 验收模式保持不变(采集一帧 JSON 到 stdout)。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"runtime"
	"syscall"
	"time"

	"github.com/YCJE/XProbe/agent/internal/collector"
	"github.com/YCJE/XProbe/agent/internal/config"
	"github.com/YCJE/XProbe/agent/internal/configsync"
	"github.com/YCJE/XProbe/agent/internal/fingerprint"
	"github.com/YCJE/XProbe/agent/internal/register"
	"github.com/YCJE/XProbe/agent/internal/reporter"
	"github.com/YCJE/XProbe/agent/internal/state"
	"github.com/YCJE/XProbe/agent/internal/tlsconf"
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
	cpu := collector.NewCPU(src)

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

	runPersistent(cfg, *configPath, src, cpu, tracker, agent, buildReport)
}

// runPersistent 常驻模式: 注册 → WebSocket 上报 + 配置拉取。
func runPersistent(cfg *config.Config, configPath string, src collector.Sources,
	cpu *collector.CPU, tracker *state.Tracker, agent *collector.Agent,
	buildReport func() model.Report) {

	// S2: Server 地址必须 https
	serverURL, err := tlsconf.ValidateServerURL(cfg.Server)
	if err != nil {
		log.Fatalf("%v", err)
	}

	// 主机指纹(设计文档 7.5): salt + CPU 型号 + 主网卡 MAC + GOOS
	hostFP := ""
	if cfg.InstallSalt != "" {
		info := cpu.StaticInfo()
		ifaces, ierr := net.Interfaces()
		if ierr != nil {
			log.Fatalf("fingerprint: %v", ierr)
		}
		mac, merr := fingerprint.PickPrimaryMAC(ifaces)
		if merr != nil {
			log.Fatalf("fingerprint: %v", merr)
		}
		hostFP = fingerprint.Compute(cfg.InstallSalt, info.Model, mac, runtime.GOOS)
	} else {
		log.Printf("[fingerprint] install_salt missing in config: WS handshake will be rejected (403); restore install_salt or reinstall")
	}

	// 注册流程(设计文档 4.2): 无 Token 时用注册码换取, 响应中 Token 唯一一次下发
	if cfg.Token == "" {
		if cfg.RegisterCode == "" {
			log.Fatal("config: token or register_code required")
		}
		if hostFP == "" {
			log.Fatal("config: install_salt required for registration (fingerprint binding)")
		}
		_, sysInfo, _, _ := collector.NewSystem(src).Collect()
		regTLS := tlsconf.New(cfg.ServerCertFingerprints, serverURL.Hostname())
		regClient := &register.Client{
			HTTP:      &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: regTLS}},
			ServerURL: serverURL.String(),
		}
		resp, rerr := regClient.Register(context.Background(), model.RegisterRequest{
			RegisterCode:    cfg.RegisterCode,
			Hostname:        agent.Hostname(),
			HostFingerprint: hostFP,
			OS:              sysInfo.OS,
			Arch:            runtime.GOARCH,
			AgentVersion:    version.Version,
		})
		if rerr != nil {
			log.Fatalf("register: %v", rerr)
		}
		cfg.Token = resp.Token
		cfg.RegisterCode = "" // 一次性, 用后即除
		if serr := config.Save(configPath, cfg); serr != nil {
			log.Fatalf("save token to config: %v", serr)
		}
		log.Printf("registered: agent_id=%d, token saved to %s (register code consumed)", resp.AgentID, configPath)
	}

	tlsC := tlsconf.New(cfg.ServerCertFingerprints, serverURL.Hostname())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 探测目标来源: 配置拉取缓存(设计文档 4.7), Ping 采集器每轮从这里读取
	targets := &targetHolder{}
	if cached, cerr := configsync.LoadCache(filepath.Join(filepath.Dir(cfg.StateFile), "ping_targets.json")); cerr == nil {
		targets.set(cached.PingTargets)
	}

	// Ping 采集器(设计文档 4.6): privileged ICMP → unprivileged ICMP → TCP 降级链
	pingCollector := collector.NewPingCollector(collector.PingMethod(cfg.PingMethod))
	pingCollector.Targets = targets.get

	// 配置拉取: 立即一次 + 每小时; 失败保留本地缓存
	go func() {
		cachePath := filepath.Join(filepath.Dir(cfg.StateFile), "ping_targets.json")
		httpc := &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsC}}
		pull := func() {
			p, perr := configsync.Pull(ctx, httpc, serverURL.String(), cfg.Token)
			if perr != nil {
				log.Printf("[config] pull failed, keep cache: %v", perr)
				return
			}
			if serr := configsync.SaveCache(cachePath, p); serr != nil {
				log.Printf("[config] save cache: %v", serr)
				return
			}
			targets.set(p.PingTargets)
			log.Printf("[config] pulled %d ping targets", len(p.PingTargets))
		}
		pull()
		t := time.NewTicker(time.Duration(cfg.ConfigSyncInterval) * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pull()
			}
		}
	}()

	client := &reporter.Client{
		WSURL:             tlsconf.WSURL(serverURL),
		Token:             cfg.Token,
		Fingerprint:       hostFP,
		ReportInterval:    time.Duration(cfg.ReportInterval) * time.Second,
		HeartbeatInterval: 30 * time.Second,
		PingInterval:      60 * time.Second,
		Dial:              reporter.DefaultDial(tlsconf.WSURL(serverURL), tlsC, true),
		Collect: func(ctx context.Context) (model.Report, error) {
			return buildReport(), nil
		},
		PingCollect: pingCollector.Collect,
	}
	log.Printf("xprobe-agent %s started: server=%s report_interval=%ds state=%s ping_method=%s",
		version.Version, cfg.Server, cfg.ReportInterval, cfg.StateFile, cfg.PingMethod)
	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("reporter exited: %v", err)
	}
	log.Printf("shutting down")
}

// targetHolder 并发安全的探测目标缓存。
type targetHolder struct {
	mu      sync.RWMutex
	targets []model.PingTarget
}

func (h *targetHolder) get() []model.PingTarget {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.targets
}

func (h *targetHolder) set(t []model.PingTarget) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.targets = t
}
