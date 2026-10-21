import { clamp, percent } from '../format'

type Props = {
  value?: number
  label: string
  sub?: string
  size?: number
  tone?: 'accent' | 'good' | 'warn' | 'bad'
}

export function Ring({ value, label, sub, size = 92, tone }: Props) {
  const v = clamp(value ?? 0)
  const stroke = size >= 80 ? 8 : 6
  const r = (size - stroke) / 2
  const circumference = 2 * Math.PI * r
  const offset = circumference * (1 - v / 100)
  const autoTone = v >= 90 ? 'bad' : v >= 75 ? 'warn' : 'good'
  // Accent is a theme identity colour; once a value crosses the warning
  // threshold, status semantics must win over branding.
  const cls = tone === 'accent' && v >= 75 ? autoTone : (tone ?? autoTone)
  return (
    <div className={`ring-block ring-${cls}`} style={{ width: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} role="img"
        aria-label={`${label} ${percent(v, 1)}`}>
        <circle className="ring-track" cx={size / 2} cy={size / 2} r={r}
          strokeWidth={stroke} fill="none" />
        <circle className="ring-value" cx={size / 2} cy={size / 2} r={r}
          strokeWidth={stroke} fill="none" strokeLinecap="round"
          strokeDasharray={circumference} strokeDashoffset={offset}
          transform={`rotate(-90 ${size / 2} ${size / 2})`} />
      </svg>
      <div className="ring-center" style={{ height: size }}>
        <strong>{value === undefined || value === null ? '—' : v.toFixed(0)}</strong>
        <span>%</span>
      </div>
      <div className="ring-label">{label}</div>
      {sub ? <div className="ring-sub">{sub}</div> : null}
    </div>
  )
}
