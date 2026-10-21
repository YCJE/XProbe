import { useEffect, useState } from 'react'
import { api } from '../api'
import { IconArrowDown, IconArrowUp, IconLogo, IconServer } from '../components/Icons'
import { Ring } from '../components/Ring'
import { speed, timeAgo } from '../format'
import type { Agent } from '../types'

export function Public({ shareId }: { shareId: string }) {
  const [servers, setServers] = useState<Agent[]>([])
  const [title, setTitle] = useState('服务器状态')
  const [error, setError] = useState('')
  const [updated, setUpdated] = useState<number>(0)

  useEffect(() => {
    let alive = true
    const load = async () => {
      try {
        const data = await api.publicDashboard(shareId)
        if (!alive) return
        setServers(data.servers)
        setTitle(data.title || '服务器状态')
        setUpdated(Math.floor(Date.now() / 1000))
        setError('')
      } catch {
        if (alive) setError('公开页不存在或已关闭')
      }
    }
    void load()
    const timer = window.setInterval(load, 10000)
    return () => { alive = false; window.clearInterval(timer) }
  }, [shareId])

  return (
    <div className="public-shell">
      <header className="public-head">
        <div className="auth-brand"><IconLogo size={24} /> XProbe</div>
        <div>
          <h1>{title}</h1>
          <p>只读状态页 · {updated ? `更新于 ${new Date(updated * 1000).toLocaleTimeString()}` : '加载中'}</p>
        </div>
        <span className="live-pill live"><i /> 公开只读</span>
      </header>
      {error ? <div className="empty-state"><h2>{error}</h2></div> : (
        <div className="server-grid public-grid">
          {servers.map((agent) => (
            <article className={`server-card ${agent.online ? '' : 'offline'}`} key={agent.id}>
              <div className="card-top">
                <div className="card-title">
                  <span className={`status-dot ${agent.online ? 'online' : ''}`} />
                  <div><h3>{agent.display_name || agent.hostname}</h3><p>{agent.os || 'unknown'} · {agent.arch || '—'}</p></div>
                </div>
                <span className={`status-badge ${agent.online ? 'online' : ''}`}>{agent.online ? '在线' : '离线'}</span>
              </div>
              <div className="card-rings">
                <Ring value={agent.cpu_usage} label="CPU" size={82} tone="accent" />
                <Ring value={agent.mem_usage} label="内存" size={82} />
                <Ring value={agent.disk_usage} label="磁盘" size={82} />
              </div>
              <div className="card-stats">
                <div className="card-stat"><IconArrowDown size={15} className="good" /><span>{speed(agent.rx_speed)}</span></div>
                <div className="card-stat"><IconArrowUp size={15} className="warn" /><span>{speed(agent.tx_speed)}</span></div>
                <div className="card-stat"><IconServer size={15} /><span>{timeAgo(agent.last_seen)}</span></div>
              </div>
            </article>
          ))}
          {!servers.length && !error ? <div className="chart-empty">暂无节点</div> : null}
        </div>
      )}
      <footer className="footer">XProbe · 只读公开页 · 不展示 IP、Token 或配置</footer>
    </div>
  )
}
