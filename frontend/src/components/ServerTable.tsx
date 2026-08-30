import { formatBytes, latencyVar, expiresInDays } from "../lib/format";
import type { ServerInfo } from "../lib/types";
import { StatusDot } from "./ui";
import { MiniBar } from "./MiniBar";

/** 表格视图(设计文档 6.4.3: 紧凑列表, 延迟列按目标动态生成)。 */
export function ServerTable({ servers, targetNames, onClick }: {
  servers: ServerInfo[];
  targetNames: string[];
  onClick?: (s: ServerInfo) => void;
}) {
  return (
    <div className="glass overflow-x-auto">
      <table className="w-full min-w-[900px] text-sm">
        <thead>
          <tr className="border-b border-card-border text-left text-xs text-muted">
            <th className="px-4 py-3">名称</th>
            <th className="px-2 py-3">状态</th>
            <th className="px-2 py-3">位置</th>
            <th className="px-2 py-3">CPU</th>
            <th className="px-2 py-3">内存</th>
            <th className="px-2 py-3">磁盘</th>
            <th className="px-2 py-3 tnum">↓/↑</th>
            <th className="px-2 py-3">月流量</th>
            {targetNames.map((n) => (
              <th key={n} className="px-2 py-3">{n}</th>
            ))}
            <th className="px-4 py-3 text-right">到期</th>
          </tr>
        </thead>
        <tbody>
          {servers.map((s) => (
            <tr
              key={s.id}
              onClick={() => onClick?.(s)}
              className={`cursor-pointer border-b border-card-border/50 last:border-0 hover:bg-card ${!s.online ? "opacity-60" : ""}`}
            >
              <td className="max-w-[180px] truncate px-4 py-2.5 font-medium">
                {s.display_name || s.hostname}
              </td>
              <td className="px-2 py-2.5"><StatusDot online={s.online} /></td>
              <td className="px-2 py-2.5 text-xs text-muted">{s.region || s.country_code || "--"}</td>
              <td className="px-2 py-2.5"><MiniBar pct={s.cpu} /></td>
              <td className="px-2 py-2.5">
                <MiniBar pct={s.mem_total ? (s.mem_used / s.mem_total) * 100 : null} />
              </td>
              <td className="px-2 py-2.5">
                <MiniBar pct={maxDisk(s)} />
              </td>
              <td className="px-2 py-2.5 text-xs tnum text-muted">
                {formatBytes(s.rx_speed)}/s · {formatBytes(s.tx_speed)}/s
              </td>
              <td className="px-2 py-2.5 text-xs tnum text-muted">
                {formatBytes(s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes)}
              </td>
              {targetNames.map((n) => {
                const v = s.ping?.[n];
                return (
                  <td key={n} className="px-2 py-2.5 text-xs tnum">
                    {v === undefined ? (
                      <span className="text-muted">--</span>
                    ) : (
                      <span style={{ color: latencyVar(v) }}>
                        {v.toFixed(1)}ms
                        {(s.ping_loss?.[n] ?? 0) > 0 && (
                          <span className="ml-1" style={{ color: "var(--warning)" }}>
                            丢{s.ping_loss[n].toFixed(0)}%
                          </span>
                        )}
                      </span>
                    )}
                  </td>
                );
              })}
              <td className="px-4 py-2.5 text-right text-xs tnum text-muted">
                {s.expires_at ? `${expiresInDays(s.expires_at)}天` : "--"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function maxDisk(s: ServerInfo): number | null {
  if (!s.disk?.length) return null;
  return Math.max(...s.disk.map((d) => (d.total ? (d.used / d.total) * 100 : 0)));
}
