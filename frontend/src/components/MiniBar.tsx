import { usageColor } from "../lib/format";

/** 表格内 60px 迷你进度条(设计系统 MASTER §6 MiniBar)。 */
export function MiniBar({ pct }: { pct: number | null }) {
  if (pct === null || pct === undefined) return <span className="text-xs text-muted tnum">--</span>;
  const p = Math.min(Math.max(pct, 0), 100);
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-[60px] rounded-full" style={{ background: "var(--card-border)" }}>
        <div
          className="h-1.5 rounded-full transition-[width] duration-300"
          style={{ width: `${p}%`, background: usageColor(p) }}
        />
      </div>
      <span className="text-xs tnum">{pct.toFixed(0)}%</span>
    </div>
  );
}
