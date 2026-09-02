import { useEffect, useMemo, useRef, useState } from "react";
import * as echarts from "echarts/core";
import { BarChart } from "echarts/charts";
import { GridComponent, TooltipComponent, LegendComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { api } from "../lib/api";
import { formatBytes } from "../lib/format";
import { GlassCard, Input, Empty } from "../components/ui";

echarts.use([BarChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer]);

interface TrafficRow { agent_id: number; name: string; month: string; rx: number; tx: number }
interface ServerLite {
  id: number; display_name: string; hostname: string; expires_at: number;
  price_amount: number; price_currency: string; price_cycle: string; online: boolean;
}

/** 报表页(Nezha 对标): 月/年度流量汇总 + 成本合计/币种换算 + 续费提醒。 */
export function ReportsPage() {
  const [rows, setRows] = useState<TrafficRow[]>([]);
  const [servers, setServers] = useState<ServerLite[]>([]);
  const [rates, setRates] = useState<Record<string, number>>({ USD: 7.2, EUR: 7.8, JPY: 0.048 });

  useEffect(() => {
    api.get<{ rows: TrafficRow[] }>("/api/v1/report/traffic?months=12").then((r) => setRows(r.rows ?? []));
    api.get<{ servers: ServerLite[] }>("/api/v1/servers").then((r) => setServers(r.servers ?? []));
    try {
      const saved = localStorage.getItem("xprobe-fx-rates");
      if (saved) setRates(JSON.parse(saved));
    } catch { /* 忽略 */ }
  }, []);

  const months = useMemo(() => [...new Set(rows.map((r) => r.month))].sort(), [rows]);
  const agents = useMemo(() => [...new Map(rows.map((r) => [r.agent_id, r.name])).entries()], [rows]);

  const series = useMemo(() => {
    return agents.map(([id, name]) => ({
      name,
      type: "bar" as const,
      stack: "traffic",
      emphasis: { focus: "series" as const },
      data: months.map((m) => {
        const r = rows.find((x) => x.agent_id === id && x.month === m);
        return r ? Math.round((r.rx + r.tx) / (1024 * 1024 * 1024) * 100) / 100 : 0; // GB
      }),
    }));
  }, [rows, months, agents]);

  // 成本: 按币种合计 + 手动汇率折算 CNY
  const costByCurrency = useMemo(() => {
    const m: Record<string, number> = {};
    for (const s of servers) {
      if (s.price_amount <= 0) continue;
      const monthly = s.price_cycle === "yearly" ? s.price_amount / 12 : s.price_amount;
      m[s.price_currency] = (m[s.price_currency] ?? 0) + monthly;
    }
    return m;
  }, [servers]);

  const totalCNY = useMemo(() => {
    let total = 0;
    for (const [cur, amount] of Object.entries(costByCurrency)) {
      if (cur === "CNY") total += amount;
      else if (rates[cur]) total += amount * rates[cur];
      else total += amount; // 无汇率的币种按原值累计并标注
    }
    return total;
  }, [costByCurrency, rates]);

  const renewals = useMemo(
    () => servers
      .filter((s) => s.expires_at > 0)
      .map((s) => ({ ...s, days: Math.ceil((s.expires_at * 1000 - Date.now()) / 86400000) }))
      .filter((s) => s.days <= 30)
      .sort((a, b) => a.days - b.days),
    [servers],
  );

  return (
    <div className="mx-auto max-w-6xl px-4 py-6">
      <GlassCard className="mb-4">
        <h2 className="mb-2 text-sm font-semibold">月度流量汇总(近 12 个月, GB)</h2>
        {rows.length === 0 ? (
          <Empty title="暂无月度流量归档" hint="Agent 上报累计 5 分钟聚合后按月落库" />
        ) : (
          <TrafficChart months={months} series={series} visible={rows.length > 0} />
        )}
      </GlassCard>

      <div className="mb-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
        <GlassCard>
          <h2 className="mb-2 text-sm font-semibold">成本合计(月)</h2>
          <ul className="text-sm tnum">
            {Object.entries(costByCurrency).length === 0 && (
              <li className="text-xs text-muted">未配置费用(在服务器元数据中填写价格)</li>
            )}
            {Object.entries(costByCurrency).map(([cur, amount]) => (
              <li key={cur} className="flex justify-between border-b border-card-border/50 py-1.5 last:border-0">
                <span>{cur}</span>
                <span>{amount.toFixed(2)}</span>
              </li>
            ))}
          </ul>
          <div className="mt-3 border-t border-card-border pt-2 text-sm tnum">
            折合 CNY 合计:<b>{totalCNY.toFixed(2)}</b>
            {Object.keys(costByCurrency).some((c) => c !== "CNY" && !rates[c]) && (
              <span className="ml-1 text-xs text-muted">(含未配置汇率的币种原值)</span>
            )}
          </div>
          <div className="mt-2 flex flex-wrap items-center gap-2 text-xs">
            {Object.keys(costByCurrency).filter((c) => c !== "CNY").map((cur) => (
              <span key={cur} className="flex items-center gap-1">
                1 {cur} =
                <Input
                  className="w-20 tnum"
                  value={String(rates[cur] ?? "")}
                  onChange={(e) => {
                    const next = { ...rates, [cur]: Number(e.target.value) || 0 };
                    setRates(next);
                    localStorage.setItem("xprobe-fx-rates", JSON.stringify(next));
                  }}
                />
                CNY
              </span>
            ))}
          </div>
        </GlassCard>

        <GlassCard>
          <h2 className="mb-2 text-sm font-semibold">续费提醒(30 天内到期)</h2>
          {renewals.length === 0 ? (
            <Empty title="暂无临近到期" />
          ) : (
            <ul className="text-sm tnum">
              {renewals.map((s) => (
                <li key={s.id} className="flex justify-between border-b border-card-border/50 py-1.5 last:border-0">
                  <span>{s.display_name || s.hostname}</span>
                  <span style={{ color: s.days <= 7 ? "var(--danger)" : s.days <= 14 ? "var(--warning)" : "var(--muted)" }}>
                    余 {s.days} 天
                  </span>
                </li>
              ))}
            </ul>
          )}
        </GlassCard>
      </div>
    </div>
  );
}

function TrafficChart({ months, series, visible }: { months: string[]; series: object[]; visible: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!ref.current || !visible) return;
    const chart = echarts.init(ref.current);
    chart.setOption({
      animation: false,
      textStyle: { color: "var(--muted)", fontSize: 11 },
      tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
      legend: { top: 0, textStyle: { color: "var(--muted)", fontSize: 11 } },
      grid: { left: 56, right: 12, top: 32, bottom: 24 },
      xAxis: { type: "category", data: months },
      yAxis: { type: "value", name: "GB" },
      series,
    });
    const onResize = () => chart.resize();
    window.addEventListener("resize", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      chart.dispose();
    };
  }, [months, series, visible]);
  if (!visible) return null;
  return <div ref={ref} className="h-72 w-full" />;
}
