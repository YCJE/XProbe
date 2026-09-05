import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { filterSortServers, useDashboard, type ViewMode } from "../store/dashboard";
import { useDashboardWS } from "../lib/ws";
import { formatBytes } from "../lib/format";
import { Button, Empty, GlassCard, Input, Select, Skeleton } from "../components/ui";
import { ServerCard } from "../components/ServerCard";
import { MapView } from "../components/MapView";
import { ServerTable } from "../components/ServerTable";

/** 仪表盘(设计文档 6.4): 概览条 + 筛选栏 + 卡片/表格双视图(地图入口位预留 v2)。 */
export function DashboardPage({ view: viewProp = "card" }: { view?: "card" | "table" | "map" }) {
  const nav = useNavigate();
  useDashboardWS();
  const { servers, tags, view, search, tagFilter, regionFilter, ipFilter, sortBy, connected } =
    useDashboard();
  const set = useDashboard((s) => s.set);
  const load = useDashboard((s) => s.load);
  // 布局自定义(Komari 借鉴): 密度 + 显示项, localStorage 持久化
  const [layout, setLayout] = useState(() => {
    try {
      return {
        density: (localStorage.getItem("xprobe-density") || "comfortable") as "comfortable" | "compact",
        showLatency: localStorage.getItem("xprobe-lat") !== "0",
        showTraffic: localStorage.getItem("xprobe-traffic") !== "0",
        showExpiry: localStorage.getItem("xprobe-expiry") !== "0",
      };
    } catch {
      return { density: "comfortable" as const, showLatency: true, showTraffic: true, showExpiry: true };
    }
  });
  const [layoutOpen, setLayoutOpen] = useState(false);
  const setLayoutItem = (k: keyof typeof layout, v: string | boolean) => {
    const next = { ...layout, [k]: v } as typeof layout;
    setLayout(next);
    localStorage.setItem(
      k === "density" ? "xprobe-density" : k === "showLatency" ? "xprobe-lat" : k === "showTraffic" ? "xprobe-traffic" : "xprobe-expiry",
      String(v),
    );
  };

  useEffect(() => {
    load();
  }, [load]);

  const shown = useMemo(
    () => filterSortServers(servers, { search, tagFilter, regionFilter, ipFilter, sortBy }),
    [servers, search, tagFilter, regionFilter, ipFilter, sortBy],
  );

  const online = servers.filter((s) => s.online).length;
  // 均值只统计在线 Agent(NodeGet 口径; 离线 0 值会拉偏概览条)
  const avg = (f: (s: typeof servers[0]) => number) => {
    const online = servers.filter((s) => s.online);
    return online.length ? online.reduce((acc, s) => acc + f(s), 0) / online.length : 0;
  };
  const monthTraffic = servers.reduce(
    (acc, s) => acc + s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes, 0);
  const regions = [...new Set(servers.map((s) => s.country_code).filter(Boolean))];
  const targetNames = [...new Set(servers.flatMap((s) => Object.keys(s.ping ?? {})))].slice(0, 6);

  return (
    <div className="mx-auto max-w-7xl px-4 py-6">
      {/* 概览条 */}
      <GlassCard className="mb-4 flex flex-wrap items-center gap-x-6 gap-y-2 py-4 text-sm tnum">
        <span>在线 <b style={{ color: "var(--success)" }}>{online}</b>/{servers.length}</span>
        <span className="text-muted">离线 {servers.length - online}</span>
        <span className="text-muted">平均 CPU {avg((s) => s.cpu ?? 0).toFixed(0)}%</span>
        <span className="text-muted">
          平均内存 {avg((s) => (s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0)).toFixed(0)}%
        </span>
        <span className="text-muted">本月总流量 {formatBytes(monthTraffic)}</span>
        {!connected && <span className="text-xs" style={{ color: "var(--warning)" }}>连接已断开, 正在重连…</span>}
      </GlassCard>

      {/* 筛选栏 */}
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Input
          placeholder="搜索 名称/位置/供应商"
          value={search}
          onChange={(e) => set("search", e.target.value)}
          className="w-56"
        />
        <Select value={tagFilter ?? ""} onChange={(e) => set("tagFilter", e.target.value ? Number(e.target.value) : null)}>
          <option value="">全部标签</option>
          {tags.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
        </Select>
        <Select value={regionFilter} onChange={(e) => set("regionFilter", e.target.value)}>
          <option value="">全部地区</option>
          {regions.map((r) => <option key={r} value={r}>{r}</option>)}
        </Select>
        <Select value={ipFilter} onChange={(e) => set("ipFilter", e.target.value as "all" | "v4" | "v6")}>
          <option value="all">全部 IP</option>
          <option value="v4">IPv4</option>
          <option value="v6">IPv6</option>
        </Select>
        <Select value={sortBy} onChange={(e) => set("sortBy", e.target.value as "name" | "cpu" | "mem" | "traffic" | "latency" | "expires")}>
          <option value="name">按名称</option>
          <option value="cpu">按 CPU</option>
          <option value="mem">按内存</option>
          <option value="traffic">按流量</option>
          <option value="latency">按延迟</option>
          <option value="expires">按到期</option>
        </Select>
        <div className="ml-auto flex gap-1">
          <Button variant={view === "card" ? "primary" : "ghost"} onClick={() => { nav("/dashboard"); set("view", "card"); }}>卡片</Button>
          <Button variant={view === "table" ? "primary" : "ghost"} onClick={() => { nav("/dashboard"); set("view", "table"); }}>表格</Button>
          <Button variant={viewProp === "map" ? "primary" : "ghost"} onClick={() => nav("/dashboard/map")}>地图</Button>
          <Button variant="ghost" onClick={() => setLayoutOpen(!layoutOpen)}>布局</Button>
        </div>
      </div>

      {layoutOpen && (
        <GlassCard className="mb-4 flex flex-wrap items-center gap-4 py-3 text-xs">
          <span className="flex items-center gap-1">
            密度
            <Select value={layout.density} onChange={(e) => setLayoutItem("density", e.target.value)}>
              <option value="comfortable">舒适</option>
              <option value="compact">紧凑</option>
            </Select>
          </span>
          {([["showLatency", "延迟格子"], ["showTraffic", "月流量"], ["showExpiry", "到期费用"]] as const).map(([k, label]) => (
            <label key={k} className="flex items-center gap-1">
              <input type="checkbox" checked={layout[k]} onChange={(e) => setLayoutItem(k, e.target.checked)} />
              {label}
            </label>
          ))}
        </GlassCard>
      )}

      {/* 视图 */}
      {viewProp === "map" ? (
        servers.length === 0 ? (
          <GlassCard><Empty title="还没有服务器" /></GlassCard>
        ) : (
          <MapView servers={shown} />
        )
      ) : servers.length === 0 ? (
        <GlassCard>
          <Empty
            title="还没有服务器"
            hint="在设置页生成注册码, 并在被控服务器执行一键安装命令"
          />
        </GlassCard>
      ) : shown.length === 0 ? (
        <GlassCard><Empty title="无匹配结果" hint="调整筛选条件后重试" /></GlassCard>
      ) : view === "card" ? (
        <div className={`grid grid-cols-1 gap-4 md:grid-cols-2 ${layout.density === "compact" ? "xl:grid-cols-4" : "xl:grid-cols-3"}`}>
          {shown.map((s) => (
            <ServerCard
              key={s.id}
              s={s}
              tagMap={new Map(tags.map((t) => [t.id, t]))}
              onClick={() => nav(`/server/${s.id}`)}
              layout={layout}
            />
          ))}
        </div>
      ) : (
        <ServerTable servers={shown} targetNames={targetNames} onClick={(s) => nav(`/server/${s.id}`)} />
      )}
    </div>
  );
}

export function DashboardSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-4 p-6 md:grid-cols-2 xl:grid-cols-3">
      {[0, 1, 2].map((i) => (
        <Skeleton key={i} className="h-72" />
      ))}
    </div>
  );
}
