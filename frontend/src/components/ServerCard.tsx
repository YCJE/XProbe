import { formatBytes, formatSpeed, formatUptime, expiresInDays, formatDate, currencySymbol } from "../lib/format";
import type { ServerInfo } from "../lib/types";
import { Badge, GlassCard, StatusDot } from "./ui";
import { RingProgress } from "./RingProgress";
import { LatencyGrid } from "./LatencyGrid";

export interface CardLayout {
  density: "comfortable" | "compact";
  showLatency: boolean;
  showTraffic: boolean;
  showExpiry: boolean;
}

const flagEmoji = (cc: string) =>
  cc && cc.length === 2
    ? String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 127397 + c.charCodeAt(0)))
    : "🌐";

/** 服务器卡片(设计系统 MASTER §6 + 设计文档 6.4.2)。 */
export function ServerCard({ s, tagMap, onClick, layout }: {
  s: ServerInfo;
  tagMap: Map<number, import("../lib/types").Tag>;
  onClick?: () => void;
  layout?: CardLayout;
}) {
  const name = s.display_name || s.hostname;
  const memPct = s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0;
  const diskPct = s.disk?.length
    ? Math.max(...s.disk.map((d) => (d.total ? (d.used / d.total) * 100 : 0)))
    : 0;
  const quotaPct = s.traffic_quota_bytes > 0
    ? ((s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes) / s.traffic_quota_bytes) * 100
    : 0;
  const days = expiresInDays(s.expires_at);
  const expiredSoon = days <= 7;
  const expiredWarn = days <= 30;
  const showTraffic = !layout || layout.showTraffic;
  const showLatency = !layout || layout.showLatency;
  const showExpiry = !layout || layout.showExpiry;

  return (
    <GlassCard hover onClick={onClick} className={`flex flex-col gap-3 ${!s.online ? "opacity-60" : ""}`}>
      {/* 头部 */}
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span aria-hidden>{flagEmoji(s.country_code)}</span>
            <span className="truncate text-sm font-semibold">{name}</span>
            {s.tags.map((id) => {
              const t = tagMap.get(id);
              return t ? <Badge key={id} color={t.color}>{t.name}</Badge> : null;
            })}
          </div>
          <div className="mt-1 flex items-center gap-2 text-xs text-muted tnum">
            <StatusDot online={s.online} />
            {s.online ? `在线 · ${formatUptime(s.uptime)}` : "离线"}
            {s.isp && <span>· {s.isp}</span>}
          </div>
        </div>
      </div>

      {/* 指标区: 三圆环 */}
      <div className="flex items-center justify-around py-1">
        <RingProgress label="CPU" value={s.cpu} size={64} />
        <RingProgress label="内存" value={memPct} size={64} />
        <RingProgress label="磁盘" value={diskPct} size={64} />
      </div>

      {/* 流量区 */}
      {showTraffic && (
        <>
          <div className="flex items-center justify-between text-xs tnum">
            <span style={{ color: "var(--link)" }}>↓ {formatSpeed(s.rx_speed)}</span>
            <span style={{ color: "var(--success)" }}>↑ {formatSpeed(s.tx_speed)}</span>
          </div>
          <div>
            <div className="mb-1 flex justify-between text-xs text-muted tnum">
              <span>
                月流量 {formatBytes(s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes)}
                {s.traffic_quota_bytes > 0 && ` / ${formatBytes(s.traffic_quota_bytes)}`}
              </span>
              {quotaPct >= 80 && (
                <span style={{ color: quotaPct >= 100 ? "var(--danger)" : "var(--warning)" }}>
                  {quotaPct.toFixed(0)}%
                </span>
              )}
            </div>
            <div className="h-1.5 rounded-full" style={{ background: "var(--card-border)" }}>
              <div
                className="h-1.5 rounded-full transition-[width] duration-300"
                style={{
                  width: `${Math.min(quotaPct, 100)}%`,
                  background: quotaPct >= 100 ? "var(--danger)" : quotaPct >= 80 ? "var(--warning)" : "var(--primary)",
                }}
              />
            </div>
          </div>
        </>
      )}

      {/* 延迟格子(卡片内嵌小格) */}
      {showLatency && (
        <LatencyGrid
          online={s.online}
          lines={Object.keys(s.ping ?? {}).map((name) => ({
            name,
            points: [{ ts: s.last_seen, avg: s.ping[name], loss: s.ping_loss?.[name] ?? 0, jitter: 0, min: 0, max: 0 }],
          }))}
        />
      )}

      {/* 尾部: 到期与费用 */}
      {showExpiry && (
        <div className="flex justify-between border-t border-card-border pt-2 text-xs tnum">
          <span style={{ color: expiredSoon ? "var(--danger)" : expiredWarn ? "var(--warning)" : "var(--muted)" }}>
            {s.expires_at
              ? `到期 ${formatDate(s.expires_at)}${days !== Infinity ? ` (余${days}天${expiredSoon ? "⚠" : ""})` : ""}`
              : "长期有效"}
          </span>
          {s.price_amount > 0 && (
            <span className="text-muted">
              {currencySymbol[s.price_currency] ?? ""}
              {s.price_amount}/{s.price_cycle === "yearly" ? "年" : "月"}
            </span>
          )}
        </div>
      )}
    </GlassCard>
  );
}
