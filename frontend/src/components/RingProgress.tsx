import { usageColor } from "../lib/format";

/** SVG 圆环指标(设计系统 MASTER §6 RingProgress)。 */
export function RingProgress({
  label, value, size = 64,
}: {
  label: string;
  /** 0-100; null/undefined 显示 -- (CPU 首采样/离线) */
  value: number | null | undefined;
  size?: number;
}) {
  const stroke = 4;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const pct = value === null || value === undefined ? 0 : Math.min(Math.max(value, 0), 100);
  const color = value === null || value === undefined ? "var(--lat-6)" : usageColor(pct);
  const showVal = value === null || value === undefined ? "--" : `${Math.round(pct)}`;

  return (
    <div className="flex flex-col items-center gap-1">
      <svg width={size} height={size} role="img" aria-label={`${label} ${showVal}%`}>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="var(--card-border)" strokeWidth={stroke} />
        <circle
          cx={size / 2} cy={size / 2} r={r} fill="none"
          stroke={color} strokeWidth={stroke} strokeLinecap="round"
          strokeDasharray={c} strokeDashoffset={c * (1 - pct / 100)}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
          style={{ transition: "stroke-dashoffset 300ms cubic-bezier(0.2,0,0,1), stroke 300ms" }}
        />
        <text
          x="50%" y="54%" textAnchor="middle" dominantBaseline="middle"
          fill="var(--foreground)" fontSize={size / 3.4} className="tnum"
        >
          {showVal}
        </text>
      </svg>
      <span className="text-xs text-muted">{label}</span>
    </div>
  );
}
