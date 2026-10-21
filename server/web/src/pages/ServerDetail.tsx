import { useCallback, useEffect, useState } from 'react'
import { api, ApiError } from '../api'
import { Layout } from '../components/Layout'
import { IconActivity, IconArrowDown, IconArrowUp, IconChevronLeft, IconCpu, IconDisk, IconMemory, IconRefresh, IconServer } from '../components/Icons'
import { LatencyGrid } from '../components/LatencyGrid'
import { LineChart } from '../components/LineChart'
import { Ring } from '../components/Ring'
import { bytes, clock, percent, speed, timeAgo, uptime } from '../format'
import type { History, ServerDetail as Detail, Session } from '../types'

type Props = {
  id: number
  session: Session
  navigate: (path: string) => void
  onLogout: () => void
  onUnauthorized: () => void
}

const RANGES = [
  { id: '1h', label: '1H' }, { id: '6h', label: '6H' }, { id: '12h', label: '12H' },
  { id: '1d', label: '1D' }, { id: '2d', label: '2D' },
]

export function ServerDetail({ id, session, navigate, onLogout, onUnauthorized }: Props) {
  const [detail, setDetail] = useState<Detail | null>(null)
  const [history, setHistory] = useState<History | null>(null)
  const [range, setRange] = useState('1h')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [metaError, setMetaError] = useState('')
  const [form, setForm] = useState({
    display_name: '', region: '', country_code: '', isp: '', expires_at: '',
    price_amount: '0', price_currency: 'CNY', price_cycle: 'monthly', traffic_quota_gb: '0',
  })

  const load = useCallback(async () => {
    try {
      const [d, h] = await Promise.all([api.server(id), api.history(id, range)])
      setDetail(d)
      setHistory(h)
      setError('')
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized()
      else if (err instanceof ApiError && err.status === 404) setError('节点不存在')
      else setError(err instanceof Error ? err.message : '加载失败')
    } finally {
      setLoading(false)
    }
  }, [id, range, onUnauthorized])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const timer = window.setInterval(() => { void load() }, 15000)
    return () => window.clearInterval(timer)
  }, [load])

  const openEdit = () => {
    if (!detail) return
    setMetaError('')
    setForm({
      display_name: detail.display_name || '',
      region: detail.region || '',
      country_code: detail.country_code || '',
      isp: detail.isp || '',
      expires_at: detail.expires_at ? new Date(detail.expires_at * 1000).toISOString().slice(0, 10) : '',
      price_amount: String(detail.price_amount ?? 0),
      price_currency: detail.price_currency || 'CNY',
      price_cycle: detail.price_cycle || 'monthly',
      traffic_quota_gb: String(detail.traffic_quota_bytes ? (detail.traffic_quota_bytes / (1024 ** 3)).toFixed(0) : 0),
    })
    setEditing(true)
  }

  const saveMeta = async () => {
    setSaving(true)
    setMetaError('')
    try {
      await api.updateServerMeta(id, {
        display_name: form.display_name,
        region: form.region,
        country_code: form.country_code,
        isp: form.isp,
        expires_at: form.expires_at ? Math.floor(new Date(`${form.expires_at}T00:00:00`).getTime() / 1000) : 0,
        price_amount: Number(form.price_amount) || 0,
        price_currency: form.price_currency,
        price_cycle: form.price_cycle,
        traffic_quota_bytes: Math.round((Number(form.traffic_quota_gb) || 0) * (1024 ** 3)),
      })
      setEditing(false)
      await load()
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized()
      else setMetaError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  if (loading && !detail) {
    return <Layout session={session} active="dashboard" navigate={navigate} onLogout={onLogout}>
      <div className="skeleton-grid">{Array.from({ length: 4 }).map((_, i) => <div className="skeleton-card" key={i} />)}</div>
    </Layout>
  }
  if (!detail) {
    return <Layout session={session} active="dashboard" navigate={navigate} onLogout={onLogout}>
      <div className="empty-state"><h2>{error || '加载失败'}</h2>
        <button className="button ghost" onClick={() => navigate('/')}>返回概览</button></div>
    </Layout>
  }

  const name = detail.display_name || detail.hostname
  const metrics = history?.metrics ?? []
  const cpuSeries = metrics.map((m) => ({ x: m.timestamp, y: m.cpu }))
  const memSeries = metrics.map((m) => ({ x: m.timestamp, y: m.mem }))
  const usedTraffic = (detail.traffic_monthly?.rx_bytes ?? 0) + (detail.traffic_monthly?.tx_bytes ?? 0)

  return (
    <Layout session={session} active="dashboard" navigate={navigate} onLogout={onLogout}>
      <section className="detail-head">
        <button className="icon-button back" onClick={() => navigate('/')} aria-label="返回"><IconChevronLeft /></button>
        <div className="detail-title">
          <div className="detail-name">
            <span className={`status-dot ${detail.online ? 'online' : ''}`} />
            <h1>{name}</h1>
            <span className={`status-badge ${detail.online ? 'online' : ''}`}>{detail.online ? '在线' : '离线'}</span>
          </div>
          <p>{detail.hostname} · {detail.os || 'unknown'} · {detail.arch || '—'} · Agent {detail.agent_version || '—'}</p>
        </div>
        <div className="page-actions">
          <span className="muted">最后上报 {timeAgo(detail.last_seen)}</span>
          <button className="button ghost" onClick={openEdit}>编辑</button>
          <button className="button ghost" onClick={() => void load()}><IconRefresh size={16} /> 刷新</button>
        </div>
      </section>

      {error ? <div className="notice error">{error}</div> : null}

      <section className="detail-top">
        <div className="panel rings-panel">
          <Ring value={detail.cpu_usage} label="CPU" size={112} tone="accent" sub={`${detail.cpu_usage?.toFixed(1) ?? '—'}%`} />
          <Ring value={detail.mem_usage} label="内存" size={112} sub={detail.memory_total ? `${bytes(detail.memory_used)} / ${bytes(detail.memory_total)}` : undefined} />
          <Ring value={detail.disk_usage} label="磁盘" size={112} />
        </div>
        <div className="panel stat-panel">
          <div className="stat-line"><IconArrowDown size={16} className="good" /><span>下行</span><strong>{speed(detail.rx_speed)}</strong></div>
          <div className="stat-line"><IconArrowUp size={16} className="warn" /><span>上行</span><strong>{speed(detail.tx_speed)}</strong></div>
          <div className="stat-line"><IconActivity size={16} /><span>负载</span><strong>{detail.load_1?.toFixed(2) ?? '—'} / {detail.load_5?.toFixed(2) ?? '—'} / {detail.load_15?.toFixed(2) ?? '—'}</strong></div>
          <div className="stat-line"><IconServer size={16} /><span>运行</span><strong>{uptime(detail.uptime)}</strong></div>
          <div className="stat-line"><IconMemory size={16} /><span>进程</span><strong>{detail.process_count ?? '—'}</strong></div>
          <div className="stat-line"><IconActivity size={16} /><span>连接</span><strong>{detail.tcp_connections ?? '—'} TCP / {detail.udp_connections ?? '—'} UDP</strong></div>
        </div>
        <div className="panel info-panel">
          <h3>节点信息</h3>
          <dl>
            <div><dt>IPv4</dt><dd>{detail.ipv4 || '—'}</dd></div>
            <div><dt>IPv6</dt><dd>{detail.ipv6 || '—'}</dd></div>
            <div><dt>地区</dt><dd>{detail.region || '—'}{detail.country_code ? ` · ${detail.country_code}` : ''}</dd></div>
            <div><dt>供应商</dt><dd>{detail.isp || '—'}</dd></div>
            <div><dt>月流量</dt><dd>{bytes(detail.traffic_monthly?.rx_bytes)} ↓ / {bytes(detail.traffic_monthly?.tx_bytes)} ↑</dd></div>
            <div><dt>到期</dt><dd>{detail.expires_at ? clock(detail.expires_at) : '—'}</dd></div>
          </dl>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head">
          <div><h2>资源趋势</h2><p>CPU 与内存使用率</p></div>
          <div className="segmented">
            {RANGES.map((r) => <button key={r.id} className={range === r.id ? 'active' : ''} onClick={() => setRange(r.id)}>{r.label}</button>)}
          </div>
        </div>
        <LineChart height={280} series={[
          { label: 'CPU', color: 'var(--accent)', points: cpuSeries },
          { label: '内存', color: 'var(--accent-2)', points: memSeries },
        ]} />
      </section>

      <section className="panel">
        <div className="panel-head">
          <div><h2>网络延迟</h2><p>最近 {range.toUpperCase()} 的探测结果，颜色越暖延迟越高</p></div>
          <span className="muted">{history?.pings.length ?? 0} 个目标</span>
        </div>
        <LatencyGrid series={history?.pings ?? []} />
      </section>

      <section className="detail-bottom">
        <div className="panel">
          <div className="panel-head"><div><h2>磁盘</h2><p>按挂载点</p></div></div>
          <div className="disk-list">
            {(detail.disks ?? []).length ? detail.disks!.map((d) => (
              <div className="disk-row" key={d.device + d.mount}>
                <div className="disk-head"><strong>{d.mount || d.device}</strong><span>{bytes(d.used)} / {bytes(d.total)} · {percent(d.percent, 1)}</span></div>
                <div className="disk-bar"><i style={{ width: `${Math.min(100, d.percent)}%` }} className={d.percent >= 90 ? 'bad' : d.percent >= 75 ? 'warn' : ''} /></div>
              </div>
            )) : <div className="chart-empty">暂无磁盘数据</div>}
          </div>
        </div>
        <div className="panel">
          <div className="panel-head"><div><h2>流量</h2><p>{detail.traffic_monthly?.month || '本月'}</p></div></div>
          <div className="traffic-total">
            <div><IconArrowDown size={18} className="good" /><span>接收</span><strong>{bytes(detail.traffic_monthly?.rx_bytes)}</strong></div>
            <div><IconArrowUp size={18} className="warn" /><span>发送</span><strong>{bytes(detail.traffic_monthly?.tx_bytes)}</strong></div>
            <div><IconDisk size={18} /><span>合计</span><strong>{bytes(usedTraffic)}</strong></div>
          </div>
        </div>
      </section>

      <p className="detail-footnote"><IconCpu size={14} /> 实时数据来自 Agent 单向上报；本页面不提供任何远程操作。</p>

      {editing ? (
        <div className="modal-backdrop" onMouseDown={() => setEditing(false)}>
          <div className="modal" role="dialog" aria-modal="true" aria-labelledby="edit-title"
            onMouseDown={(e) => e.stopPropagation()}>
            <div className="modal-head">
              <div><h2 id="edit-title">编辑节点信息</h2><p>这些字段只影响面板展示，不会改变 Agent 行为。</p></div>
              <button className="icon-button" onClick={() => setEditing(false)} aria-label="关闭">✕</button>
            </div>
            {metaError ? <div className="notice error">{metaError}</div> : null}
            <div className="form-grid">
              <label className="field"><span>显示名称</span><input value={form.display_name}
                onChange={(e) => setForm({ ...form, display_name: e.target.value })} /></label>
              <label className="field"><span>地区</span><input value={form.region}
                onChange={(e) => setForm({ ...form, region: e.target.value })} placeholder="上海 / Tokyo" /></label>
              <label className="field"><span>国家代码</span><input value={form.country_code}
                onChange={(e) => setForm({ ...form, country_code: e.target.value.toUpperCase().slice(0, 2) })} placeholder="CN" /></label>
              <label className="field"><span>供应商</span><input value={form.isp}
                onChange={(e) => setForm({ ...form, isp: e.target.value })} /></label>
              <label className="field"><span>到期日</span><input type="date" value={form.expires_at}
                onChange={(e) => setForm({ ...form, expires_at: e.target.value })} /></label>
              <label className="field"><span>周期费用</span><input type="number" value={form.price_amount}
                onChange={(e) => setForm({ ...form, price_amount: e.target.value })} /></label>
              <label className="field"><span>币种</span><input value={form.price_currency}
                onChange={(e) => setForm({ ...form, price_currency: e.target.value.toUpperCase().slice(0, 4) })} /></label>
              <label className="field"><span>计费周期</span>
                <select value={form.price_cycle} onChange={(e) => setForm({ ...form, price_cycle: e.target.value })}>
                  <option value="monthly">月付</option><option value="yearly">年付</option><option value="one-time">一次性</option>
                </select></label>
              <label className="field"><span>月流量配额 (GiB，0 不限)</span><input type="number" value={form.traffic_quota_gb}
                onChange={(e) => setForm({ ...form, traffic_quota_gb: e.target.value })} /></label>
            </div>
            <div className="modal-foot">
              <span>保存后立即生效</span>
              <button className="button primary" disabled={saving} onClick={() => void saveMeta()}>
                {saving ? '保存中…' : '保存'}
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </Layout>
  )
}
