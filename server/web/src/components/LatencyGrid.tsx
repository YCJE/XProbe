import { latency, latencyTone } from '../format'
import type { PingSeries } from '../types'

type Props = { series: PingSeries[]; columns?: number }

export function LatencyGrid({ series, columns = 60 }: Props) {
  if (!series.length) {
    return <div className="chart-empty">暂无延迟数据</div>
  }
  return (
    <div className="latency-list">
      {series.map((s) => {
        const points = s.points.slice(-columns)
        const pad = Math.max(0, columns - points.length)
        const last = points[points.length - 1]
        const tone = latencyTone(last?.avg, last?.loss)
        return (
          <div className="latency-row" key={`${s.target}-${s.ip_version}`}>
            <div className="latency-meta">
              <strong>{s.name || s.target}</strong>
              <span>{s.method?.toUpperCase()} · IPv{s.ip_version}</span>
            </div>
            <div className="latency-grid" style={{ gridTemplateColumns: `repeat(${columns}, minmax(2px, 1fr))` }}>
              {Array.from({ length: pad }).map((_, i) => <i className="cell empty" key={`pad-${i}`} />)}
              {points.map((p, i) => {
                const t = latencyTone(p.avg, p.loss)
                return (
                  <i key={`${p.timestamp}-${i}`}
                    className={`cell ${p.loss >= 100 ? 'loss' : t}`}
                    title={`${new Date(p.timestamp * 1000).toLocaleTimeString()} · ${latency(p.avg)} · 丢包 ${p.loss.toFixed(0)}%`}
                    style={{ opacity: p.avg < 0 ? 0.25 : Math.max(0.35, Math.min(1, 1.1 - p.avg / 400)) }} />
                )
              })}
            </div>
            <div className={`latency-current ${tone}`}>
              <strong>{last?.loss >= 100 ? '丢包' : latency(last?.avg)}</strong>
              <span>{last ? `${last.loss.toFixed(0)}% loss` : '—'}</span>
            </div>
          </div>
        )
      })}
    </div>
  )
}
