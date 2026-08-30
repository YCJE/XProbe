import { latencyVar } from "../lib/format";

export interface LatencyPoint {
  ts: number;
  avg: number;   // ms; NaN 表示该次超时/失败
  loss: number;
  jitter: number;
  min: number;
  max: number;
}

/** 延迟格子图(设计文档 6.4.4 + 设计系统 MASTER §6 LatencyGrid)。 */
export function LatencyGrid({
  lines, maxBars = 24, online = true,
}: {
  /** 每行一条线路 */
  lines: { name: string; points: LatencyPoint[] }[];
  maxBars?: number;
  online?: boolean;
}) {
  if (!lines.length) return null;
  return (
    <div className="flex flex-col gap-1">
      {lines.map((line) => (
        <Line key={line.name} line={line} maxBars={maxBars} online={online} />
      ))}
    </div>
  );
}

function Line({ line, maxBars, online }: { line: { name: string; points: LatencyPoint[] }; maxBars: number; online: boolean }) {
  const pts = line.points.slice(-maxBars);
  const valid = pts.filter((p) => Number.isFinite(p.avg));
  const max = Math.max(...valid.map((p) => p.avg), 1);
  const cur = pts.length ? pts[pts.length - 1] : null;
  const curColor = !online || !cur || !Number.isFinite(cur.avg) ? "var(--lat-6)" : latencyVar(cur.avg);

  return (
    <div className="flex items-center gap-2 text-xs">
      <span className="w-16 shrink-0 truncate text-muted">{line.name}</span>
      <div className="flex h-5 flex-1 items-end gap-[2px]" role="img" aria-label={`${line.name} 延迟格子`}>
        {pts.length === 0 && (
          <div className="h-1 w-full rounded-sm" style={{ background: "var(--lat-6)", opacity: 0.3 }} />
        )}
        {pts.map((p, i) => {
          const failed = !Number.isFinite(p.avg);
          const h = failed || !online ? 4 : 4 + Math.max((p.avg / max) * 16, 0.5);
          return (
            <div
              key={i}
              title={
                failed
                  ? `${fmtTime(p.ts)} · 超时/失败`
                  : `${fmtTime(p.ts)} · 平均 ${p.avg.toFixed(1)}ms (最小 ${p.min.toFixed(1)} 最大 ${p.max.toFixed(1)} 抖动 ${p.jitter.toFixed(1)}) · 丢包 ${p.loss.toFixed(0)}%`
              }
              className="min-w-[4px] flex-1 rounded-[1px] transition-[height] duration-300"
              style={{
                height: h,
                background: failed || !online ? "var(--lat-6)" : latencyVar(p.avg),
                opacity: failed || !online ? 0.45 : 0.9,
              }}
            />
          );
        })}
      </div>
      <span className="w-24 shrink-0 text-right tnum" style={{ color: curColor }}>
        {cur && Number.isFinite(cur.avg) ? `${cur.avg.toFixed(1)}ms ${cur.loss > 0 ? `丢${cur.loss.toFixed(0)}%` : ""}` : "超时"}
      </span>
    </div>
  );
}

function fmtTime(ts: number): string {
  const d = new Date(ts * 1000);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}
