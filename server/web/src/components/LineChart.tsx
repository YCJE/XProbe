import { useMemo, useState } from 'react'

export type ChartPoint = { x: number; y: number }
export type ChartSeries = { label: string; color: string; points: ChartPoint[]; area?: boolean }

type Props = {
  series: ChartSeries[]
  height?: number
  minY?: number
  maxY?: number
  formatY?: (value: number) => string
  formatX?: (value: number) => string
  emptyText?: string
}

const PAD = { left: 42, right: 16, top: 18, bottom: 26 }
const W = 720

export function LineChart({
  series, height = 240, minY = 0, maxY = 100,
  formatY = (v) => `${v.toFixed(0)}%`,
  formatX = (v) => new Date(v * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
  emptyText = '暂无历史数据',
}: Props) {
  const [hover, setHover] = useState<number | null>(null)
  const all = useMemo(() => series.flatMap((s) => s.points), [series])
  const xMin = all.length ? Math.min(...all.map((p) => p.x)) : 0
  const xMax = all.length ? Math.max(...all.map((p) => p.x)) : 1
  const top = Math.max(maxY, ...all.map((p) => p.y), 1)
  const h = Math.max(120, height)
  const innerW = W - PAD.left - PAD.right
  const innerH = h - PAD.top - PAD.bottom
  const x = (v: number) => PAD.left + (xMax === xMin ? innerW / 2 : ((v - xMin) / (xMax - xMin)) * innerW)
  const y = (v: number) => PAD.top + innerH - ((v - minY) / (top - minY || 1)) * innerH

  if (!all.length) {
    return <div className="chart-empty" style={{ height }}>{emptyText}</div>
  }

  const hoverPoint = hover !== null ? all[Math.min(hover, all.length - 1)] : null
  const grid = [0, 0.25, 0.5, 0.75, 1]

  return (
    <div className="chart-wrap">
      <svg className="line-chart" viewBox={`0 0 ${W} ${h}`} role="img" aria-label="历史趋势">
        <defs>
          {series.map((s, i) => (
            <linearGradient key={s.label} id={`area-${i}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={s.color} stopOpacity="0.28" />
              <stop offset="100%" stopColor={s.color} stopOpacity="0" />
            </linearGradient>
          ))}
        </defs>
        {grid.map((g) => {
          const value = minY + (top - minY) * (1 - g)
          return (
            <g key={g}>
              <line className="chart-grid" x1={PAD.left} x2={W - PAD.right}
                y1={PAD.top + innerH * g} y2={PAD.top + innerH * g} />
              <text className="chart-axis" x={PAD.left - 8} y={PAD.top + innerH * g + 4}
                textAnchor="end">{formatY(value)}</text>
            </g>
          )
        })}
        <text className="chart-axis" x={PAD.left} y={h - 6}>{formatX(xMin)}</text>
        <text className="chart-axis" x={W - PAD.right} y={h - 6} textAnchor="end">{formatX(xMax)}</text>
        {series.map((s, i) => {
          const pts = s.points.map((p) => `${x(p.x)},${y(p.y)}`).join(' ')
          const line = s.points.map((p) => `${x(p.x)},${y(p.y)}`).join(' ')
          const area = `${PAD.left},${PAD.top + innerH} ${line} ${PAD.left + innerW},${PAD.top + innerH}`
          return (
            <g key={s.label}>
              {s.area !== false ? <polygon points={area} fill={`url(#area-${i})`} /> : null}
              <polyline points={pts} fill="none" stroke={s.color} strokeWidth="2.2"
                strokeLinejoin="round" strokeLinecap="round" />
            </g>
          )
        })}
        {hoverPoint ? (
          <g className="chart-hover">
            <line x1={x(hoverPoint.x)} x2={x(hoverPoint.x)} y1={PAD.top} y2={PAD.top + innerH} />
            <circle cx={x(hoverPoint.x)} cy={y(hoverPoint.y)} r="4.5" />
            <text x={Math.min(x(hoverPoint.x) + 10, W - 150)} y={PAD.top + 12}>
              {formatX(hoverPoint.x)} · {formatY(hoverPoint.y)}
            </text>
          </g>
        ) : null}
        <rect x={PAD.left} y={PAD.top} width={innerW} height={innerH} fill="transparent"
          onMouseLeave={() => setHover(null)}
          onMouseMove={(event) => {
            const rect = event.currentTarget.getBoundingClientRect()
            const px = ((event.clientX - rect.left) / rect.width) * innerW
            const target = xMin + (px / innerW) * (xMax - xMin)
            let best = 0
            for (let i = 1; i < all.length; i++) {
              if (Math.abs(all[i].x - target) < Math.abs(all[best].x - target)) best = i
            }
            setHover(best)
          }} />
      </svg>
      <div className="chart-legend">
        {series.map((s) => (
          <span key={s.label}><i style={{ background: s.color }} />{s.label}</span>
        ))}
      </div>
    </div>
  )
}
