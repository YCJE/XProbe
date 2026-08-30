import { useEffect, useState } from "react";
import { api, ApiError } from "../lib/api";
import type { AlertHistoryItem, AlertRule } from "../lib/types";
import { Button, Empty, GlassCard, Input, Select } from "../components/ui";

/** 告警管理(设计文档 6.6): 规则 CRUD + 历史时间线; 后端在 M5 上线。 */
export function AlertsPage() {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const [history, setHistory] = useState<AlertHistoryItem[]>([]);
  const [err, setErr] = useState("");

  const load = async () => {
    try {
      const [r, h] = await Promise.all([
        api.get<{ rules: AlertRule[] }>("/api/v1/alerts"),
        api.get<{ history: AlertHistoryItem[] }>("/api/v1/alerts/history"),
      ]);
      setRules(r.rules ?? []);
      setHistory(h.history ?? []);
      setErr("");
    } catch (e) {
      setErr(e instanceof ApiError && e.status === 404 ? "" : String(e));
    }
  };
  useEffect(() => { load(); }, []);

  return (
    <div className="mx-auto max-w-5xl px-4 py-6">
      <GlassCard className="mb-4">
        <div className="mb-3 flex items-center justify-between">
          <h2 className="text-sm font-semibold">告警规则</h2>
          <NewRule onDone={load} />
        </div>
        <RuleTable rules={rules} onDone={load} />
      </GlassCard>
      <GlassCard>
        <h2 className="mb-3 text-sm font-semibold">告警历史</h2>
        {history.length === 0 ? (
          <Empty title="暂无告警记录" hint="规则触发 FIRING/RESOLVED 后在此展示时间线" />
        ) : (
          <ul className="text-xs tnum">
            {history.map((h) => (
              <li key={h.id} className="flex justify-between border-b border-card-border/50 py-2 last:border-0">
                <span>
                  规则 #{h.rule_id} · Agent #{h.agent_id}
                  {h.value !== null && ` · 值 ${h.value.toFixed(1)}`}
                </span>
                <span style={{ color: h.status === "FIRING" ? "var(--danger)" : "var(--success)" }}>
                  {h.status} · {new Date(h.updated_at * 1000).toLocaleString()}
                </span>
              </li>
            ))}
          </ul>
        )}
      </GlassCard>
      {err && <p className="mt-2 text-xs text-muted">告警接口尚未就绪({err})</p>}
    </div>
  );
}

function RuleTable({ rules, onDone }: { rules: AlertRule[]; onDone: () => void }) {
  if (rules.length === 0) return <Empty title="暂无告警规则" hint="示例: CPU > 80% 持续 5 分钟" />;
  return (
    <table className="w-full text-sm">
      <thead><tr className="border-b border-card-border text-left text-xs text-muted">
        <th className="py-2">名称</th><th>指标</th><th>条件</th><th>持续</th><th>状态</th><th />
      </tr></thead>
      <tbody>
        {rules.map((r) => (
          <tr key={r.id} className="border-b border-card-border/50 last:border-0">
            <td className="py-2">{r.name}</td>
            <td className="mono text-xs">{r.metric}</td>
            <td className="text-xs tnum">{r.operator} {r.threshold}</td>
            <td className="text-xs tnum">{r.duration}s</td>
            <td className="text-xs">{r.enabled ? "启用" : "停用"}</td>
            <td className="text-right">
              <Button variant="ghost" onClick={async () => { await api.del(`/api/v1/alerts/${r.id}`); onDone(); }}>删除</Button>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function NewRule({ onDone }: { onDone: () => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [metric, setMetric] = useState("cpu_usage");
  const [threshold, setThreshold] = useState("80");
  const [duration, setDuration] = useState("300");

  const create = async () => {
    await api.post("/api/v1/alerts", {
      name, metric, operator: ">", threshold: Number(threshold), duration: Number(duration),
      enabled: true, notify_channel_id: null,
    });
    setOpen(false); setName("");
    onDone();
  };

  if (!open) return <Button onClick={() => setOpen(true)}>新建规则</Button>;
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Input placeholder="名称" value={name} onChange={(e) => setName(e.target.value)} className="w-36" />
      <Select value={metric} onChange={(e) => setMetric(e.target.value)}>
        <option value="cpu_usage">CPU</option>
        <option value="mem_usage">内存</option>
        <option value="disk_usage">磁盘</option>
        <option value="traffic_quota">流量配额</option>
        <option value="expire_days">到期天数</option>
      </Select>
      <Input className="w-20 tnum" value={threshold} onChange={(e) => setThreshold(e.target.value)} />
      <Input className="w-24 tnum" value={duration} onChange={(e) => setDuration(e.target.value)} title="持续秒数" />
      <Button onClick={create} disabled={!name}>保存</Button>
      <Button variant="ghost" onClick={() => setOpen(false)}>取消</Button>
    </div>
  );
}
