# Agent 资源占用基准

> 方法论 + 当前实测。目标:Agent 稳态占用尽量低(Komari/Nezha 的核心卖点之一),后续优化以此为准绳。

## 测量方法

```bash
go build -o xprobe-agent ./agent/cmd/agent
./xprobe-agent --config config.yml &   # 常驻模式(连接失败重连循环也在内)
sleep 8
# Windows: Get-Process xprobe-agent | Select WorkingSet64, PrivateMemorySize64
# Linux:   ps -o rss=,vsz= -C xprobe-agent  (RSS 单位 KB)
```

注意:重连失败循环会持续拨号,内存/CPU 略高于稳态连接;生产环境(连接正常)预期更低。

## 当前实测(Windows 11, Go 1.26, amd64, v0.1.0-dev)

| 指标 | 值 |
|------|-----|
| 二进制体积 | 10.9 MB(strip 前) |
| RSS (WorkingSet) | ~51 MB |
| Private Memory | ~46 MB |
| 线程数 | 9 |
| CPU | 近似 0(空闲重连循环) |

## 结论与优化方向

当前 RSS ~50MB 高于同类(主要来自 Go 运行时堆 + pro-bing/TLS 栈)。优化候选项:

1. `GOGC`/`GOMEMLIMIT` 调优(如 `GOMEMLIMIT=32MiB` 通过 systemd Environment 注入)
2. 上报帧复用缓冲,减少 3s JSON 序列化垃圾
3. 环形缓冲预分配已在 Server 侧;Agent 侧无大缓冲
4. Linux 实机复测(目标:稳态 RSS < 25MB),达标后再对外宣传"低占用"

## Server 侧参考

Server 单实例设计规模为 10-20 Agent;聚合器/环形缓冲内存开销 ≈ Agent 数 × (3600×帧大小) ≈ 每台 < 5MB,SQLite 仅落聚合数据。
