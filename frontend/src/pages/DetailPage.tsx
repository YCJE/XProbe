import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import * as echarts from "echarts/core";
import { LineChart } from "echarts/charts";
import { GridComponent, TooltipComponent, LegendComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { api, type DetailResp, type HistoryResponse } from "../lib/api";
import type { ServerInfo } from "../lib/types";
import { formatBytes, formatSpeed, latencyVar, formatUptime } from "../lib/format";
import { GlassCard, StatusDot } from "../components/ui";
import { LatencyGrid, type LatencyPoint } from "../components/LatencyGrid";

echarts.use([LineChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer]);

const RANGES = ["1h", "6h", "12h", "1d", "2d", "7d", "30d"] as const;

/** 服务器详情页(设计文档 6.5): 延迟格子置顶 + 实时折线 + 月流量 + 信息费用。 */
export function DetailPage() {
  const { id } = useParams();
  const nav = useNavigate();
  const [detail, setDetail] = useState<DetailResp | null>(null);
  const [history, setHistory] = useState<HistoryResponse | null>(null);
  const [range, setRange] = useState<(typeof RANGES)[number]>("1h");

  useEffect(() => {
    api.get<DetailResp>(`/api/v1/servers/${id}`).then(setDetail).catch(() => nav("/dashboard"));
  }, [id, nav]);

  useEffect(() => {
    api.get<HistoryResponse>(`/api/v1/servers/${id}/history?range=${range}`).then(setHistory);
  }, [id, range]);

  if (!detail) {
    return <div className="flex h-screen items-center justify-center text-sm text-muted">加载中…</div>;
  }
  const s: ServerInfo = detail.server;

  return (
    <div className="mx-auto max-w-7xl px-4 py-6">
      <button className="mb-4 text-sm text-link" onClick={() => nav("/dashboard")}>
        ← 返回
      </button>
      <GlassCard className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1 py-4">
        <span className="text-base font-semibold">{s.display_name || s.hostname}</span>
        <span className="flex items-center gap-1.5 text-xs text-muted tnum">
          <StatusDot online={s.online} />
          {s.online ? `在线 · ${formatUptime(s.uptime)}` : "离线"}
        </span>
        <span className="text-xs text-muted">{s.os} · {s.arch} · {s.cores} 核</span>
        {s.ipv4 && <span className="mono text-xs text-muted">IPv4 {s.ipv4}</span>}
        {s.ipv6 && <span className="mono text-xs text-muted">IPv6 {s.ipv6}</span>}
        <span className="ml-auto flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className="rounded-lg px-2.5 py-1 text-xs"
              style={r === range
                ? { background: "var(--primary)", color: "var(--primary-fg)" }
                : { border: "1px solid var(--card-border)" }}
            >
              {r}
            </button>
          ))}
        </span>
      </GlassCard>

      <ChartCard title="CPU 占用率" series={[{ name: "CPU %", color: "var(--primary)" }]}
        points={linePoints(history)} yMax={100} />
      <ChartCard title="内存占用率" series={[{ name: "内存 %", color: "var(--success)" }]}
        points={memPoints(history, s)} yMax={100} />
      <ChartCard title="网络流量" series={[
        { name: "下行", color: "var(--link)" }, { name: "上行", color: "var(--success)" }]}
        points={netPoints(history)} bytes />

      <GlassCard className="mb-4">
        <h3 className="mb-3 text-sm font-semibold">延迟格子 (最近 60 分钟)</h3>
        <LatencyGrid lines={detailPingLines(detail)} maxBars={60} online={s.online} />
        {Object.keys(s.ping ?? {}).length === 0 && (
          <p className="text-xs text-muted">暂无探测数据(M4 接入后展示)</p>
        )}
      </GlassCard>

      <GlassCard>
        <h3 className="mb-3 text-sm font-semibold">信息与费用</h3>
        <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-xs sm:grid-cols-3">
          <Info label="Agent" value={s.agent_version} />
          <Info label="TCP / UDP 连接" value={`${s.tcp_connections} / ${s.udp_connections}`} />
          <Info label="进程数" value={String(s.process_count)} />
          <Info label="本月流量" value={formatBytes(s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes)} />
          {s.traffic_quota_bytes > 0 && <Info label="流量配额" value={formatBytes(s.traffic_quota_bytes)} />}
          {s.price_amount > 0 && <Info label="费用" value={`${s.price_amount} ${s.price_currency}/${s.price_cycle}`} />}
        </dl>
        {detail.traffic_monthly.length > 0 && (
          <p className="mt-3 text-xs text-muted tnum">
            近月流量: {detail.traffic_monthly.map((t) => `${t.month} ${formatBytes(t.rx_bytes + t.tx_bytes)}`).join(" · ")}
          </p>
        )}
      </GlassCard>
    </div>
  );
}

function Info({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-muted">{label}</dt>
      <dd className="tnum">{value || "--"}</dd>
    </div>
  );
}

function detailPingLines(detail: DetailResp): { name: string; points: LatencyPoint[] }[] {
  const s = detail.server;
  return Object.keys(s.ping ?? {}).map((name) => ({
    name,
    points: [{ ts: Date.now() / 1000, avg: s.ping[name], loss: s.ping_loss?.[name] ?? 0, jitter: 0, min: 0, max: 0 }],
  }));
}

// —— ECharts 通用卡片 ——
type Pt = { x: number | string; cpu?: number; mem?: number; rx?: number; tx?: number };

function linePoints(h: HistoryResponse | null): Pt[] {
  if (h?.realtime) return h.realtime.map((r) => ({ x: r.timestamp, cpu: r.data.cpu.usage ?? 0 }));
  if (h?.points_5m) return h.points_5m.map((p) => ({ x: p.timestamp, cpu: p.cpu }));
  if (h?.points_daily) return h.points_daily.map((p) => ({ x: p.date, cpu: p.cpu_avg }));
  return [];
}
function memPoints(h: HistoryResponse | null, s: ServerInfo): Pt[] {
  if (h?.realtime) return h.realtime.map((r) => ({ x: r.timestamp, mem: r.data.memory.total ? (r.data.memory.used / r.data.memory.total) * 100 : 0 }));
  if (h?.points_5m) return h.points_5m.map((p) => ({ x: p.timestamp, mem: p.mem }));
  if (h?.points_daily) return h.points_daily.map((p) => ({ x: p.date, mem: p.mem_avg }));
  return s.mem_total ? [{ x: Date.now(), mem: (s.mem_used / s.mem_total) * 100 }] : [];
}
function netPoints(h: HistoryResponse | null): Pt[] {
  if (h?.realtime) return h.realtime.map((r) => ({ x: r.timestamp, rx: 0, tx: 0 }));
  if (h?.points_5m) return h.points_5m.map((p) => ({ x: p.timestamp, rx: p.rx, tx: p.tx }));
  if (h?.points_daily) return h.points_daily.map((p) => ({ x: p.date, rx: p.rx, tx: p.tx }));
  return [];
}

function ChartCard({ title, series, points, yMax, bytes }: {
  title: string;
  series: { name: string; color: string }[];
  points: Pt[];
  yMax?: number;
  bytes?: boolean;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const dark = document.documentElement.classList.contains("dark");

  useEffect(() => {
    const chart = echarts.init(ref.current!);
    return () => chart.dispose();
  }, []);

  useEffect(() => {
    const chart = echarts.init(ref.current!);
    const axis = points.map((p) => p.x);
    const fmt = (v: number) => (bytes ? formatBytes(v) : `${v.toFixed(1)}`);
    chart.setOption({
      animation: false,
      textStyle: { color: "var(--muted)", fontSize: 11 },
      tooltip: { trigger: "axis", valueFormatter: fmt },
      legend: { top: 0, right: 0, textStyle: { color: "var(--muted)", fontSize: 11 } },
      grid: { left: 48, right: 12, top: 28, bottom: 24 },
      xAxis: { type: "category", data: axis, axisLabel: { hideOverlap: true } },
      yAxis: { type: "value", max: yMax, axisLabel: { formatter: fmt } },
      series: series.map((s, i) => {
        const key = ["cpu", "mem", "rx", "tx"][["CPU %", "内存 %", "下行", "上行"].indexOf(s.name)] ?? "cpu";
        return {
          name: s.name, type: "line" as const, showSymbol: false, smooth: true,
          data: points.map((p) => (p as Record<string, number>)[key] ?? 0),
          lineStyle: { color: s.color, width: 1.5 },
          itemStyle: { color: s.color },
        };
      }),
    });
    const onResize = () => chart.resize();
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      chart.dispose();
    };
  }, [points, series, yMax, bytes, dark]);

  return (
    <GlassCard className="mb-4">
      <h3 className="mb-2 text-sm font-semibold">{title}</h3>
      <div ref={ref} className="h-52 w-full" />
    </GlassCard>
  );
}

export { latencyVar };
