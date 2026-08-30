import { formatBytes, formatSpeed, formatUptime, usageColor, expiresInDays, formatDate, currencySymbol } from "../lib/format";
import type { ServerInfo } from "../lib/types";
import { Badge, GlassCard, StatusDot } from "./ui";
import { RingProgress } from "./RingProgress";
import { LatencyGrid } from "./LatencyGrid";

const flagEmoji = (cc: string) =>
  cc && cc.length === 2
    ? String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 127397 + c.charCodeAt(0)))
    : "🌐";

/** 服务器卡片(设计系统 MASTER §6 + 设计文档 6.4.2)。 */
export function ServerCard({ s, tagMap, onClick }: {
  s: ServerInfo;
  tagMap: Map<number, import("../lib/types").Tag>;
  onClick?: () => void;
}) {
  const name = s.display_name || s.hostname;
  const memPct = s.mem_total ? (s.mem_used / s.mem_total) * 100 : 0;
  const diskPct = s.disk?.length
    ? (s.disk.reduce((m, d) => Math.max(m, d.total ? d.used / d.total : 0), 0) as number) * 100
    : 0;
  const rx = s.rx_speed, tx = s.tx_speed;
  const quotaPct = s.traffic_quota_bytes > 0
    ? (s.traffic_monthly.rx_bytes + s.traffic_monthly.tx_bytes) / s.traffic_quota_bytes * 100
    : 0;
  const days = expiresInDays(s.expires_at);
  const expiredSoon = days <= 7;
  const expiredWarn = days <= 30;

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
      <div className="flex items-center justify-between text-xs tnum">
        <span style={{ color: "var(--link)" }}>↓ {formatSpeed(rx)}</span>
        <span style={{ color: "var(--success)" }}>↑ {formatSpeed(tx)}</span>
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

      {/* 延迟格子(卡片内嵌小格) */}
      <LatencyGrid ping={s.ping} loss={s.ping_loss} online={s.online} rows={3} />

      {/* 尾部: 到期与费用 */}
      <div className="flex justify-between border-t border-card-border pt-2 text-xs tnum">
        <span style={{ color: expiredSoon ? "var(--danger)" : expiredWarn ? "var(--warning)" : "var(--muted)" }}>
          {s.expires_at ? `到期 ${formatDate(s.expires_at)}${days !== Infinity ? ` (余${days}天${expiredSoon ? "⚠" : ""})` : ""}` : "长期有效"}
        </span>
        {s.price_amount > 0 && (
          <span className="text-muted">
            {currencySymbol[s.price_currency] ?? ""}{s.price_amount}/{s.price_cycle === "yearly" ? "年" : "月"}
          </span>
        )}
      </div>
    </GlassCard>
  );
}
