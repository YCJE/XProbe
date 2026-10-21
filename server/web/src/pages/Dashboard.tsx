import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError, dashboardSocketURL } from '../api'
import { Layout } from '../components/Layout'
import { IconCopy, IconPlus, IconRefresh, IconSearch, IconServer } from '../components/Icons'
import { MiniMeter, ServerCard } from '../components/ServerCard'
import { bytes, percent, speed, timeAgo, uptime } from '../format'
import type { Agent, RegisterCode, Session } from '../types'

type Props = {
  session: Session
  navigate: (path: string) => void
  onLogout: () => void
  onUnauthorized: () => void
}

type View = 'cards' | 'table'
type Status = 'all' | 'online' | 'offline'
type Sort = 'name' | 'cpu' | 'mem' | 'last'

export function Dashboard({ session, navigate, onLogout, onUnauthorized }: Props) {
  const [servers, setServers] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [view, setView] = useState<View>('cards')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<Status>('all')
  const [sort, setSort] = useState<Sort>('name')
  const [live, setLive] = useState<'connecting' | 'live' | 'polling'>('connecting')
  const [showAdd, setShowAdd] = useState(false)
  const [code, setCode] = useState<RegisterCode | null>(null)
  const [addError, setAddError] = useState('')
  const [copied, setCopied] = useState(false)
  const [publicURL, setPublicURL] = useState(location.origin)

  const load = useCallback(async () => {
    try {
      const data = await api.servers()
      setServers(data.servers)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized()
      else setError(err instanceof Error ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [onUnauthorized])

  useEffect(() => { void load() }, [load])

  useEffect(() => {
    let alive = true
    let socket: WebSocket | null = null
    let retry: number | undefined
    let poll: number | undefined

    const connect = () => {
      if (!alive) return
      const ws = new WebSocket(dashboardSocketURL())
      socket = ws
      ws.onopen = () => { if (alive) setLive('live') }
      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(String(event.data)) as { servers?: Agent[] }
          if (Array.isArray(data.servers)) { setServers(data.servers); setLoading(false) }
        } catch { /* ignore malformed frame */ }
      }
      ws.onclose = () => {
        if (!alive) return
        setLive('polling')
        retry = window.setTimeout(connect, 4000)
      }
      ws.onerror = () => ws.close()
    }
    connect()
    poll = window.setInterval(() => {
      if (!socket || socket.readyState !== WebSocket.OPEN) void load()
    }, 5000)

    return () => {
      alive = false
      if (retry) window.clearTimeout(retry)
      if (poll) window.clearInterval(poll)
      socket?.close()
    }
  }, [load])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    const list = servers.filter((s) => {
      if (status === 'online' && !s.online) return false
      if (status === 'offline' && s.online) return false
      if (!q) return true
      return [s.hostname, s.display_name, s.os, s.region, s.isp, s.ipv4, s.ipv6]
        .some((value) => (value || '').toLowerCase().includes(q))
    })
    list.sort((a, b) => {
      if (sort === 'name') return (a.display_name || a.hostname).localeCompare(b.display_name || b.hostname)
      if (sort === 'cpu') return (b.cpu_usage ?? -1) - (a.cpu_usage ?? -1)
      if (sort === 'mem') return (b.mem_usage ?? -1) - (a.mem_usage ?? -1)
      return (b.last_seen ?? 0) - (a.last_seen ?? 0)
    })
    return list
  }, [servers, query, status, sort])

  const online = servers.filter((s) => s.online).length
  const avgCpu = online
    ? servers.filter((s) => s.online && s.cpu_usage !== undefined)
      .reduce((sum, s) => sum + (s.cpu_usage ?? 0), 0) / Math.max(1, servers.filter((s) => s.online && s.cpu_usage !== undefined).length)
    : 0

  const openAdd = async () => {
    setShowAdd(true)
    setCopied(false)
    setAddError('')
    try {
      const settings = await api.settings()
      setPublicURL((settings.public_url || location.origin).replace(/\/$/, ''))
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) { onUnauthorized(); return }
    }
    if (code && code.expires_at * 1000 <= Date.now()) setCode(null)
    if (code && code.expires_at * 1000 > Date.now()) return
    try {
      setCode(await api.createRegisterCode())
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) { onUnauthorized(); return }
      setAddError(err instanceof Error ? err.message : '生成注册码失败')
    }
  }

  const safeOrigin = (() => {
    try {
      const parsed = new URL(publicURL)
      if (parsed.protocol === 'https:' || parsed.protocol === 'http:') return parsed.origin
    } catch { /* fall through to the current origin */ }
    return location.origin
  })()
  const command = code
    ? `curl -fsSL ${safeOrigin}/install-agent.sh | sudo sh -s -- --server ${safeOrigin} --code ${code.code}`
    : ''

  const copy = async (value: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch { setAddError('复制失败，请手动选择文本') }
  }

  return (
    <Layout session={session} active="dashboard" navigate={navigate} onLogout={onLogout}>
      <section className="page-head">
        <div>
          <h1>概览</h1>
          <p>所有被监控节点的实时状态。数据由 Agent 单向上报。</p>
        </div>
        <div className="page-actions">
          <span className={`live-pill ${live}`}>
            <i /> {live === 'live' ? '实时' : live === 'polling' ? '轮询' : '连接中'}
          </span>
          <button className="button ghost" onClick={() => void load()}><IconRefresh size={16} /> 刷新</button>
          <button className="button primary" onClick={() => void openAdd()}><IconPlus size={16} /> 添加节点</button>
        </div>
      </section>

      <section className="summary-grid">
        <div className="summary-card">
          <span>节点总数</span><strong>{servers.length}</strong><small>台服务器</small>
        </div>
        <div className="summary-card">
          <span>在线</span><strong className="good">{online}</strong><small>{servers.length ? `${Math.round((online / servers.length) * 100)}% 在线率` : '等待节点'}</small>
        </div>
        <div className="summary-card">
          <span>离线</span><strong className={servers.length - online > 0 ? 'bad' : ''}>{servers.length - online}</strong><small>需要关注</small>
        </div>
        <div className="summary-card">
          <span>平均 CPU</span><strong>{online ? percent(avgCpu, 0) : '—'}</strong><small>在线节点</small>
        </div>
      </section>

      <section className="toolbar">
        <label className="search">
          <IconSearch size={16} />
          <input placeholder="搜索主机名、IP、地区…" value={query} onChange={(e) => setQuery(e.target.value)} />
        </label>
        <div className="segmented" role="group" aria-label="状态筛选">
          {(['all', 'online', 'offline'] as Status[]).map((s) => (
            <button key={s} className={status === s ? 'active' : ''} onClick={() => setStatus(s)}>
              {s === 'all' ? '全部' : s === 'online' ? '在线' : '离线'}
            </button>
          ))}
        </div>
        <select className="select" value={sort} onChange={(e) => setSort(e.target.value as Sort)} aria-label="排序">
          <option value="name">按名称</option>
          <option value="cpu">按 CPU</option>
          <option value="mem">按内存</option>
          <option value="last">按最后上报</option>
        </select>
        <div className="segmented view-toggle" role="group" aria-label="视图">
          <button className={view === 'cards' ? 'active' : ''} onClick={() => setView('cards')}>卡片</button>
          <button className={view === 'table' ? 'active' : ''} onClick={() => setView('table')}>表格</button>
        </div>
      </section>

      {error ? <div className="notice error">{error}</div> : null}
      {loading ? (
        <div className="skeleton-grid">{Array.from({ length: 6 }).map((_, i) => <div className="skeleton-card" key={i} />)}</div>
      ) : servers.length === 0 ? (
        <div className="empty-state">
          <div className="empty-icon"><IconServer size={40} /></div>
          <h2>还没有节点</h2>
          <p>生成一次性注册码，在服务器上运行安装命令。Agent 只会上报数据，不会接受任何远程命令。</p>
          <button className="button primary" onClick={() => void openAdd()}><IconPlus size={16} /> 添加第一台节点</button>
        </div>
      ) : filtered.length === 0 ? (
        <div className="empty-state compact"><h2>没有匹配的节点</h2><p>换个关键词或筛选条件试试。</p></div>
      ) : view === 'cards' ? (
        <div className="server-grid">
          {filtered.map((agent) => (
            <ServerCard key={agent.id} agent={agent} onClick={() => navigate(`/servers/${agent.id}`)} />
          ))}
        </div>
      ) : (
        <div className="table-wrap">
          <table className="server-table">
            <thead>
              <tr>
                <th>节点</th><th>CPU</th><th>内存</th><th>磁盘</th><th>下行</th><th>上行</th><th>运行时间</th><th>最后上报</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((agent) => (
                <tr key={agent.id} onClick={() => navigate(`/servers/${agent.id}`)} tabIndex={0}
                  onKeyDown={(e) => { if (e.key === 'Enter') navigate(`/servers/${agent.id}`) }}>
                  <td>
                    <div className="table-node">
                      <span className={`status-dot ${agent.online ? 'online' : ''}`} />
                      <div><strong>{agent.display_name || agent.hostname}</strong><span>{agent.ipv4 || agent.os || '—'}</span></div>
                    </div>
                  </td>
                  <td><MiniMeter value={agent.cpu_usage} /></td>
                  <td><MiniMeter value={agent.mem_usage} /></td>
                  <td><MiniMeter value={agent.disk_usage} /></td>
                  <td className="mono">{speed(agent.rx_speed)}</td>
                  <td className="mono">{speed(agent.tx_speed)}</td>
                  <td>{uptime(agent.uptime)}</td>
                  <td>{timeAgo(agent.last_seen)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {showAdd ? (
        <div className="modal-backdrop" onMouseDown={() => setShowAdd(false)}>
          <div className="modal" role="dialog" aria-modal="true" aria-labelledby="add-title"
            onMouseDown={(e) => e.stopPropagation()}>
            <div className="modal-head">
              <div><h2 id="add-title">添加节点</h2><p>注册码 15 分钟内有效，只能使用一次。</p></div>
              <button className="icon-button" onClick={() => setShowAdd(false)} aria-label="关闭">✕</button>
            </div>
            {addError ? <div className="notice error">{addError}</div> : null}
            {code ? (
              <>
                <div className="code-box">
                  <span>注册码</span>
                  <strong>{code.code}</strong>
                  <button className="button ghost" onClick={() => void copy(code.code)}>
                    <IconCopy size={15} /> {copied ? '已复制' : '复制'}
                  </button>
                </div>
                <p className="field-hint">在目标 Linux 服务器上运行：</p>
                <pre className="command">{command}</pre>
                <button className="button ghost wide" onClick={() => void copy(command)}>
                  <IconCopy size={15} /> 复制安装命令
                </button>
                <p className="field-hint">如果安装脚本尚未部署，可直接把注册码交给 Agent 配置文件。节点上线后会出现在这里。</p>
              </>
            ) : (
              <div className="modal-loading">正在生成注册码…</div>
            )}
            <div className="modal-foot">
              <span>{code ? `过期时间 ${new Date(code.expires_at * 1000).toLocaleTimeString()}` : ''}</span>
              <button className="button primary" onClick={() => { setShowAdd(false); setCode(null) }}>完成</button>
            </div>
          </div>
        </div>
      ) : null}
    </Layout>
  )
}
