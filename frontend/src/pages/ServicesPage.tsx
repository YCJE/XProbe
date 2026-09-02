import { useEffect, useState } from "react";
import { api, ApiError } from "../lib/api";
import type { PingResult } from "../lib/types";
import { latencyVar } from "../lib/format";
import { Button, Empty, GlassCard, Input, Select } from "../components/ui";

export interface ServiceCfg {
  id: number; name: string; type: "http" | "tcp" | "icmp"; target: string;
  port: number; path: string; interval_sec: number; enabled: boolean; notify_channel_id: number | null;
}
export interface ServiceStatusInfo {
  id: number; name: string; type: string; up: boolean; latency_ms: number;
  uptime_45d: number; recent: { ts: number; ok: boolean; latency_ms: number }[];
  daily: { date: string; total: number; ok: number; up_ratio: number }[];
}
interface ServiceRow extends ServiceCfg { status?: ServiceStatusInfo }

/** 服务监控页(Nezha 对标): 拨测配置 + 实时状态 + 45 天在线率。 */
export function ServicesPage() {
  const [rows, setRows] = useState<ServiceRow[]>([]);
  const [err, setErr] = useState("");

  const load = async () => {
    try {
      const r = await api.get<{ services: ServiceRow[] }>("/api/v1/services");
      setRows(r.services ?? []);
      setErr("");
    } catch (e) {
      setErr(e instanceof ApiError && e.status === 404 ? "" : String(e));
    }
  };
  useEffect(() => { load(); }, []);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6">
      <NewService onDone={load} />
      {rows.length === 0 ? (
        <GlassCard className="mt-4">
          <Empty title="暂无服务监控" hint="添加 HTTP/TCP/ICMP 端点, Server 主动拨测产出在线率(与 Agent 通道无关)" />
        </GlassCard>
      ) : (
        <div className="mt-4 flex flex-col gap-4">
          {rows.map((r) => <ServiceCard key={r.id} row={r} onDone={load} />)}
        </div>
      )}
      {err && <p className="mt-2 text-xs text-muted">服务接口异常({err})</p>}
    </div>
  );
}

function ServiceCard({ row, onDone }: { row: ServiceRow; onDone: () => void }) {
  const st = row.status;
  const up = st?.up ?? false;
  return (
    <GlassCard>
      <div className="flex items-center justify-between">
        <div>
          <span className="text-sm font-semibold">{row.name}</span>
          <span className="ml-2 text-xs text-muted">
            {row.type.toUpperCase()}
            {row.type !== "http" && row.port ? `:${row.port}` : ""}
          </span>
        </div>
        <div className="flex items-center gap-3 text-xs tnum">
          {st && (
            <>
              <span style={{ color: up ? "var(--success)" : "var(--danger)" }}>
                {up ? `在线 ${st.latency_ms.toFixed(0)}ms` : "不可达"}
              </span>
              <span className="text-muted">45 天在线率 {st.uptime_45d.toFixed(1)}%</span>
            </>
          )}
          <Button variant="ghost" onClick={async () => { await api.del(`/api/v1/services/${row.id}`); onDone(); }}>删除</Button>
        </div>
      </div>
      {/* 45 天在线率日格 */}
      {st?.daily && st.daily.length > 0 && (
        <div className="mt-3 flex flex-wrap gap-[3px]" title="近 45 天每日在线率">
          {st.daily.map((d) => (
            <div
              key={d.date}
              title={`${d.date}: ${d.up_ratio.toFixed(1)}% (${d.ok}/${d.total})`}
              className="h-4 w-4 rounded-[3px]"
              style={{ background: dayColor(d.up_ratio) }}
            />
          ))}
        </div>
      )}
      {/* 最近 64 次结果条 */}
      {st?.recent && st.recent.length > 0 && (
        <div className="mt-2 flex gap-[2px]" title="最近探测结果">
          {st.recent.map((r, i) => (
            <div
              key={i}
              title={`${new Date(r.ts * 1000).toLocaleString()} ${r.ok ? `${r.latency_ms.toFixed(0)}ms` : "失败"}`}
              className="h-1.5 flex-1 rounded-sm"
              style={{ background: r.ok ? "var(--success)" : "var(--danger)", opacity: 0.8 }}
            />
          ))}
        </div>
      )}
    </GlassCard>
  );
}

function dayColor(ratio: number): string {
  if (ratio >= 99.5) return "var(--lat-1)";
  if (ratio >= 95) return "var(--lat-2)";
  if (ratio >= 85) return "var(--lat-3)";
  if (ratio >= 50) return "var(--lat-4)";
  return "var(--lat-5)";
}

function NewService({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState<ServiceCfg["type"]>("http");
  const [target, setTarget] = useState("");
  const [port, setPort] = useState("");
  const [path, setPath] = useState("");
  const [interval, setIntervalSec] = useState("60");
  const [testResult, setTestResult] = useState("");

  const create = async () => {
    try {
    const body = {
      name, type, target,
      port: type === "http" ? 0 : Number(port || 80),
      path: type === "http" ? path : "",
      interval_sec: Number(interval || 60), enabled: true, notify_channel_id: null,
    };
    const r = await api.post<{ id: number }>("/api/v1/services", body);
    // 建完立即探活验证
    const t = await api.post<{ ok: boolean; error?: string; latency_ms: number }>(`/api/v1/services/${r.id}/test`);
    setTestResult(t.ok ? `探测成功 ${t.latency_ms.toFixed(0)}ms` : `探测失败: ${t.error}`);
    setName(""); setTarget(""); setPort(""); setPath("");
    onDone();
    } catch (e) {
      setTestResult(`保存失败: ${e instanceof ApiError ? e.message : "网络错误"}`);
    }
  };

  if (!open) return <Button onClick={() => setOpen(true)}>添加服务监控</Button>;
  return (
    <GlassCard>
      <div className="flex flex-wrap items-center gap-2">
        <Input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} className="w-32" />
        <Select value={type} onChange={(e) => setType(e.target.value as ServiceCfg["type"])}>
          <option value="http">HTTP</option>
          <option value="tcp">TCP</option>
          <option value="icmp">ICMP</option>
        </Select>
        <Input
          className="w-64"
          placeholder={type === "http" ? "https://example.com" : "example.com"}
          value={target}
          onChange={(e) => setTarget(e.target.value)}
        />
        {type !== "http" && (
          <Input placeholder="端口" className="w-20 tnum" value={port} onChange={(e) => setPort(e.target.value)} />
        )}
        {type === "http" && (
          <Input placeholder="路径 /health" className="w-32" value={path} onChange={(e) => setPath(e.target.value)} />
        )}
        <Input placeholder="间隔秒" className="w-20 tnum" value={interval} onChange={(e) => setIntervalSec(e.target.value)} />
        <Button onClick={create} disabled={!name || !target}>保存并探测</Button>
        <Button variant="ghost" onClick={() => setOpen(false)}>取消</Button>
      </div>
      {testResult && <p className="mt-2 text-xs tnum" style={{ color: testResult.includes("成功") ? "var(--success)" : "var(--danger)" }}>{testResult}</p>}
      <p className="mt-2 text-xs text-muted">
        目标为管理员配置, 允许内网地址(用于局域网服务拨测); 状态转移会向所选通知渠道推送
      </p>
      <details className="mt-1 text-xs text-muted">
        <summary className="cursor-pointer">延迟色阶说明</summary>
        <PingLegend />
      </details>
    </GlassCard>
  );
}

function PingLegend() {
  const v = (ms: number) => latencyVar(ms);
  return (
    <ul className="mt-1 tnum">
      <li>&lt;50ms <span style={{ color: v(10) }}>■</span> / &lt;100ms <span style={{ color: v(80) }}>■</span> / &lt;200ms <span style={{ color: v(150) }}>■</span></li>
      <li>&lt;400ms <span style={{ color: v(300) }}>■</span> / ≥400ms <span style={{ color: v(500) }}>■</span> / 超时 <span style={{ color: "var(--lat-6)" }}>■</span></li>
    </ul>
  );
}

export type { PingResult };
