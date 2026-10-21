import { bytes, percent, speed, timeAgo, uptime } from '../format'
import type { Agent } from '../types'
import { IconArrowDown, IconArrowUp, IconCpu, IconDisk, IconMemory, IconServer } from './Icons'
import { Ring } from './Ring'

type Props = { agent: Agent; onClick: () => void }

export function ServerCard({ agent, onClick }: Props) {
  const name = agent.display_name || agent.hostname
  const usedTraffic = (agent.traffic_monthly?.rx_bytes ?? 0) + (agent.traffic_monthly?.tx_bytes ?? 0)
  const quota = agent.traffic_quota_bytes ?? 0
  const trafficPercent = quota > 0 ? Math.min(100, (usedTraffic / quota) * 100) : 0

  return (
    <article className={`server-card ${agent.online ? '' : 'offline'}`} onClick={onClick}
      role="button" tabIndex={0} onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onClick() }}>
      <div className="card-top">
        <div className="card-title">
          <span className={`status-dot ${agent.online ? 'online' : ''}`} />
          <div>
            <h3>{name}</h3>
            <p>{agent.os || 'unknown'} · {agent.arch || '—'}{agent.region ? ` · ${agent.region}` : ''}</p>
          </div>
        </div>
        <span className={`status-badge ${agent.online ? 'online' : ''}`}>{agent.online ? '在线' : '离线'}</span>
      </div>

      <div className="card-ips">
        <span>{agent.ipv4 || 'IPv4 未上报'}</span>
        {agent.ipv6 ? <span>{agent.ipv6}</span> : null}
        {agent.agent_version ? <span className="muted">v{agent.agent_version}</span> : null}
      </div>

      <div className="card-rings">
        <Ring value={agent.cpu_usage} label="CPU" size={82} tone="accent" />
        <Ring value={agent.mem_usage} label="内存" size={82} />
        <Ring value={agent.disk_usage} label="磁盘" size={82} />
      </div>

      <div className="card-stats">
        <div className="card-stat">
          <IconArrowDown size={15} className="good" />
          <span>{speed(agent.rx_speed)}</span>
        </div>
        <div className="card-stat">
          <IconArrowUp size={15} className="warn" />
          <span>{speed(agent.tx_speed)}</span>
        </div>
        <div className="card-stat">
          <IconServer size={15} />
          <span>{uptime(agent.uptime)}</span>
        </div>
      </div>

      {quota > 0 ? (
        <div className="traffic">
          <div className="traffic-head">
            <span>月流量</span>
            <span>{bytes(usedTraffic)} / {bytes(quota)}</span>
          </div>
          <div className="traffic-bar"><i style={{ width: `${trafficPercent}%` }} /></div>
        </div>
      ) : (
        <div className="traffic compact">
          <span>{agent.traffic_monthly?.month || '本月'}</span>
          <span>↓ {bytes(agent.traffic_monthly?.rx_bytes)} · ↑ {bytes(agent.traffic_monthly?.tx_bytes)}</span>
        </div>
      )}

      <div className="card-foot">
        <span>最后上报 {timeAgo(agent.last_seen)}</span>
        <span>{percent(agent.disk_usage)} 磁盘</span>
      </div>
    </article>
  )
}

export function MiniMeter({ value, label }: { value?: number; label?: string }) {
  const v = Math.max(0, Math.min(100, value ?? 0))
  return (
    <div className="mini-meter" title={label ? `${label} ${percent(value)}` : percent(value)}>
      <div className="mini-track"><i style={{ width: `${v}%` }} className={v >= 90 ? 'bad' : v >= 75 ? 'warn' : ''} /></div>
      <span>{percent(value)}</span>
    </div>
  )
}

export function IconForMetric({ metric }: { metric: string }) {
  if (metric === 'cpu') return <IconCpu size={15} />
  if (metric === 'memory') return <IconMemory size={15} />
  return <IconDisk size={15} />
}
