import { useEffect, useState } from 'react'
import { api, ApiError } from '../api'
import { Layout } from '../components/Layout'
import { IconActivity, IconAlert, IconBell, IconCheck, IconCopy, IconGlobe, IconPlus, IconShield, IconWifi, IconX } from '../components/Icons'
import { clock, timeAgo } from '../format'
import type { AlertHistory, AlertRule, AuditEntry, NotifyChannel, PingTargetConfig, Session, Settings as SettingsType, TotpSetup } from '../types'

type Props = {
  session: Session
  navigate: (path: string) => void
  onLogout: () => void
  onUnauthorized: () => void
}

type Tab = 'security' | 'alerts' | 'targets' | 'notify' | 'public' | 'audit' | 'about'

const TABS: Array<{ id: Tab; label: string; icon: typeof IconShield }> = [
  { id: 'security', label: '账户安全', icon: IconShield },
  { id: 'alerts', label: '告警规则', icon: IconAlert },
  { id: 'targets', label: '探测目标', icon: IconWifi },
  { id: 'notify', label: '通知渠道', icon: IconBell },
  { id: 'public', label: '公开页', icon: IconGlobe },
  { id: 'audit', label: '审计日志', icon: IconActivity },
  { id: 'about', label: '关于', icon: IconCheck },
]

export function Settings({ session, navigate, onLogout, onUnauthorized }: Props) {
  const [tab, setTab] = useState<Tab>('security')
  const [settings, setSettings] = useState<SettingsType>({})
  const [rules, setRules] = useState<AlertRule[]>([])
  const [alertHistory, setAlertHistory] = useState<AlertHistory[]>([])
  const [channels, setChannels] = useState<NotifyChannel[]>([])
  const [targets, setTargets] = useState<PingTargetConfig[]>([])
  const [events, setEvents] = useState<AuditEntry[]>([])
  const [sessions, setSessions] = useState<Array<Record<string, unknown>>>([])
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')

  const notify = (text: string, ok = true) => {
    setMessage(text)
    setError(ok ? '' : text)
    window.setTimeout(() => setMessage(''), 2500)
  }

  const load = async () => {
    try {
      // Settings is the auth gate: do not swallow its 401.
      const s = await api.settings()
      const [a, ah, c, t, sess, audit] = await Promise.all([
        api.alerts().catch(() => ({ rules: [] })),
        api.alertHistory().catch(() => ({ history: [] })),
        api.channels().catch(() => ({ channels: [] })),
        api.pingTargets().catch(() => ({ targets: [] })),
        api.sessions().catch(() => ({ sessions: [] })),
        api.audit().catch(() => ({ events: [] })),
      ])
      setSettings(s ?? {})
      setRules(a.rules ?? [])
      setAlertHistory(ah.history ?? [])
      setChannels(c.channels ?? [])
      setTargets(t.targets ?? [])
      setSessions(sess.sessions ?? [])
      setEvents(audit.events ?? [])
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized()
    }
  }

  useEffect(() => { void load() }, [])

  return (
    <Layout session={session} active="settings" navigate={navigate} onLogout={onLogout}>
      <section className="page-head">
        <div><h1>设置</h1><p>安全、告警和展示配置。所有修改只影响主控，不会向 Agent 下发命令。</p></div>
      </section>
      {message ? <div className="notice success">{message}</div> : null}
      {error ? <div className="notice error">{error}</div> : null}
      <div className="settings-shell">
        <aside className="settings-nav">
          {TABS.map((t) => {
            const Icon = t.icon
            return <button key={t.id} className={tab === t.id ? 'active' : ''} onClick={() => setTab(t.id)}>
              <Icon size={17} /> {t.label}
            </button>
          })}
        </aside>
        <div className="settings-body">
          {tab === 'security' ? <Security settings={settings} sessions={sessions} reload={load} notify={notify} /> : null}
          {tab === 'alerts' ? <Alerts rules={rules} history={alertHistory} reload={load} notify={notify} channels={channels} /> : null}
          {tab === 'targets' ? <Targets targets={targets} reload={load} notify={notify} /> : null}
          {tab === 'notify' ? <Notify channels={channels} reload={load} notify={notify} /> : null}
          {tab === 'public' ? <PublicPage settings={settings} reload={load} notify={notify} /> : null}
          {tab === 'audit' ? <Audit events={events} /> : null}
          {tab === 'about' ? <About version={settings.version} /> : null}
        </div>
      </div>
    </Layout>
  )
}

function Security({ settings, sessions, reload, notify }: {
  settings: SettingsType
  sessions: Array<Record<string, unknown>>
  reload: () => Promise<void>
  notify: (text: string, ok?: boolean) => void
}) {
  const [setup, setSetup] = useState<TotpSetup | null>(null)
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState<string[]>([])
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)

  const start = async () => {
    setBusy(true)
    try {
      setSetup(await api.totpSetup(password))
      setPassword('')
    } catch (err) { notify(err instanceof Error ? err.message : '操作失败', false) }
    finally { setBusy(false) }
  }
  const enable = async () => {
    setBusy(true)
    try {
      const out = await api.totpEnable(code)
      setRecovery(out.recovery_codes)
      setSetup(null)
      await reload()
      notify('两步验证已启用')
    } catch (err) { notify(err instanceof Error ? err.message : '验证码错误', false) }
    finally { setBusy(false) }
  }
  const disable = async () => {
    setBusy(true)
    try { await api.totpDisable(password); setPassword(''); setRecovery([]); await reload(); notify('两步验证已关闭') }
    catch (err) { notify(err instanceof Error ? err.message : '取消失败', false) }
    finally { setBusy(false) }
  }
  const revoke = async (id: unknown) => {
    try { await api.deleteSession(String(id)); await reload(); notify('会话已吊销') }
    catch (err) { notify(err instanceof Error ? err.message : '吊销失败', false) }
  }

  return (
    <>
      <section className="panel">
        <div className="panel-head"><div><h2>两步验证</h2><p>TOTP 验证码，可选。启用后登录需要输入 6 位动态码。</p></div>
          <span className={`status-badge ${settings.totp_enabled ? 'online' : ''}`}>{settings.totp_enabled ? '已启用' : '未启用'}</span></div>
        {recovery.length ? (
          <div className="notice success">
            <strong>恢复码只显示一次，请立即保存：</strong>
            <div className="recovery-grid">{recovery.map((r) => <code key={r}>{r}</code>)}</div>
          </div>
        ) : null}
        {settings.totp_enabled ? (
          <div className="inline-form">
            <label className="field"><span>输入当前密码以关闭</span>
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" /></label>
            <button className="button danger" disabled={busy || !password} onClick={() => void disable()}>关闭两步验证</button>
          </div>
        ) : setup ? (
          <div className="totp-setup">
            <p>在验证器 App 中添加以下密钥，然后输入生成的 6 位验证码。</p>
            <div className="code-box"><span>密钥</span><strong>{setup.secret}</strong>
              <button className="button ghost" onClick={() => void navigator.clipboard.writeText(setup.secret)}><IconCopy size={15} /> 复制</button></div>
            <pre className="command small">{setup.url}</pre>
            <div className="inline-form">
              <label className="field"><span>6 位验证码</span>
                <input value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))} inputMode="numeric" /></label>
              <button className="button primary" disabled={busy || code.length !== 6} onClick={() => void enable()}>启用</button>
              <button className="button ghost" onClick={() => setSetup(null)}>取消</button>
            </div>
          </div>
        ) : (
          <div className="inline-form">
            <label className="field"><span>当前密码（重新验证）</span>
              <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password" placeholder="启用前确认是本人操作" /></label>
            <button className="button primary" disabled={busy || !password} onClick={() => void start()}>启用两步验证</button>
          </div>
        )}
      </section>

      <section className="panel">
        <div className="panel-head"><div><h2>登录会话</h2><p>吊销后该浏览器立即退出。</p></div></div>
        <div className="table-wrap flat">
          <table className="server-table">
            <thead><tr><th>IP</th><th>客户端</th><th>创建</th><th>最后活动</th><th></th></tr></thead>
            <tbody>
              {sessions.map((s) => (
                <tr key={String(s.id)}>
                  <td className="mono">{String(s.ip || '—')}</td>
                  <td className="ellipsis">{String(s.user_agent || '—')}</td>
                  <td>{s.created_at ? clock(Number(s.created_at)) : '—'}</td>
                  <td>{s.last_seen_at ? timeAgo(Number(s.last_seen_at)) : '—'}</td>
                  <td><button className="icon-button danger" title="吊销" onClick={() => void revoke(s.id)}><IconX size={15} /></button></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}

function Alerts({ rules, history, channels, reload, notify }: {
  rules: AlertRule[]
  history: AlertHistory[]
  channels: NotifyChannel[]
  reload: () => Promise<void>
  notify: (text: string, ok?: boolean) => void
}) {
  const [name, setName] = useState('CPU 过高')
  const [metric, setMetric] = useState('cpu')
  const [operator, setOperator] = useState('>')
  const [threshold, setThreshold] = useState(90)
  const [duration, setDuration] = useState(300)
  const [selectedChannels, setSelectedChannels] = useState<number[]>([])
  const [busy, setBusy] = useState(false)

  const add = async () => {
    setBusy(true)
    try {
      const channelIDs = selectedChannels.filter((id) => channels.some((c) => c.id === id))
      await api.createAlert({
        name, metric, operator, threshold, duration, enabled: true,
        scope: { type: 'all' }, notify_channel_ids: channelIDs,
      })
      await reload()
      setSelectedChannels([])
      notify('告警规则已创建')
    } catch (err) { notify(err instanceof Error ? err.message : '创建失败', false) }
    finally { setBusy(false) }
  }
  const remove = async (id: number) => {
    try { await api.deleteAlert(id); await reload(); notify('规则已删除') }
    catch (err) { notify(err instanceof Error ? err.message : '删除失败', false) }
  }

  return (
    <>
      <section className="panel">
        <div className="panel-head"><div><h2>新建规则</h2><p>超过阈值并持续一段时间后触发；通知渠道在“通知渠道”中配置。</p></div></div>
        <div className="form-grid">
          <label className="field"><span>名称</span><input value={name} onChange={(e) => setName(e.target.value)} /></label>
          <label className="field"><span>指标</span>
            <select value={metric} onChange={(e) => setMetric(e.target.value)}>
              <option value="cpu">CPU 使用率</option><option value="memory">内存使用率</option>
              <option value="disk">磁盘使用率</option><option value="offline">离线</option>
            </select></label>
          <label className="field"><span>运算符</span>
            <select value={operator} onChange={(e) => setOperator(e.target.value)} disabled={metric === 'offline'}>
              <option value=">">&gt;</option><option value=">=">≥</option><option value="<">&lt;</option><option value="<=">≤</option>
            </select></label>
          <label className="field"><span>阈值 (%)</span>
            <input type="number" min={0} max={100} value={threshold} onChange={(e) => setThreshold(Number(e.target.value))} disabled={metric === 'offline'} /></label>
          <label className="field"><span>持续 (秒)</span>
            <input type="number" min={0} max={86400} value={duration} onChange={(e) => setDuration(Number(e.target.value))} /></label>
          <div className="field channel-field">
            <span>通知渠道</span>
            <div className="channel-picker">
              {channels.length ? channels.map((c) => (
                <label className="check" key={c.id}>
                  <input type="checkbox" checked={selectedChannels.includes(c.id)}
                    onChange={(e) => setSelectedChannels((prev) => e.target.checked
                      ? [...prev, c.id]
                      : prev.filter((id) => id !== c.id))} />
                  <span>{c.name}</span>
                  <em>{c.type}</em>
                </label>
              )) : <span className="field-hint">还没有渠道，先去「通知渠道」创建一个。</span>}
            </div>
          </div>
          <div className="form-action"><button className="button primary" disabled={busy || !name} onClick={() => void add()}><IconPlus size={16} /> 创建</button></div>
        </div>
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>规则列表</h2><p>{rules.length} 条规则 · {channels.length} 个通知渠道</p></div></div>
        <div className="table-wrap flat">
          <table className="server-table">
            <thead><tr><th>名称</th><th>条件</th><th>持续</th><th>通知</th><th>状态</th><th></th></tr></thead>
            <tbody>
              {rules.map((r) => (
                <tr key={r.id}>
                  <td><strong>{r.name}</strong></td>
                  <td className="mono">{r.metric} {r.operator} {r.threshold}</td>
                  <td>{r.duration}s</td>
                  <td>
                    {r.notify_channel_ids?.length
                      ? r.notify_channel_ids.map((id) => channels.find((c) => c.id === id)?.name ?? `#${id}`).join('、')
                      : <span className="muted">未配置</span>}
                  </td>
                  <td><span className={`status-badge ${r.enabled ? 'online' : ''}`}>{r.enabled ? '启用' : '停用'}</span></td>
                  <td><button className="icon-button danger" onClick={() => void remove(r.id)} title="删除"><IconX size={15} /></button></td>
                </tr>
              ))}
            </tbody>
          </table>
          {!rules.length ? <div className="chart-empty">还没有告警规则</div> : null}
        </div>
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>最近告警</h2><p>状态机的最近 100 条记录，重启后仍保留。</p></div></div>
        <div className="table-wrap flat">
          <table className="server-table">
            <thead><tr><th>状态</th><th>规则</th><th>节点</th><th>值</th><th>更新时间</th></tr></thead>
            <tbody>
              {history.map((h) => (
                <tr key={h.id}>
                  <td><span className={`status-badge ${h.status === 'FIRING' ? 'online' : ''}`}>{h.status}</span></td>
                  <td>#{h.rule_id}</td><td>#{h.agent_id}</td>
                  <td className="mono">{h.value.toFixed(2)}</td>
                  <td>{new Date(h.updated_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!history.length ? <div className="chart-empty">还没有告警历史</div> : null}
        </div>
      </section>
    </>
  )
}

function Targets({ targets, reload, notify }: {
  targets: PingTargetConfig[]
  reload: () => Promise<void>
  notify: (text: string, ok?: boolean) => void
}) {
  const [name, setName] = useState('Cloudflare')
  const [target, setTarget] = useState('1.1.1.1')
  const [protocol, setProtocol] = useState('icmp')
  const [ipVersion, setIpVersion] = useState('4')
  const [busy, setBusy] = useState(false)

  const add = async () => {
    setBusy(true)
    try {
      await api.createPingTarget({
        target, name, region: '', isp: '', ip_version: Number(ipVersion),
        protocol, enabled: true,
      })
      await reload()
      notify('探测目标已添加')
    } catch (err) { notify(err instanceof Error ? err.message : '添加失败', false) }
    finally { setBusy(false) }
  }
  const toggle = async (t: PingTargetConfig) => {
    try {
      await api.updatePingTarget(t.id, {
        target: t.target, name: t.name, region: t.region, isp: t.isp,
        ip_version: t.ip_version, protocol: t.protocol, enabled: !t.enabled,
      })
      await reload()
    } catch (err) { notify(err instanceof Error ? err.message : '更新失败', false) }
  }
  const remove = async (id: number) => {
    try { await api.deletePingTarget(id); await reload(); notify('目标已删除') }
    catch (err) { notify(err instanceof Error ? err.message : '删除失败', false) }
  }

  return (
    <>
      <section className="panel">
        <div className="panel-head"><div><h2>新增探测目标</h2><p>目标变更会在 Agent 下次配置拉取后生效（默认每小时）。</p></div></div>
        <div className="form-grid">
          <label className="field"><span>名称</span><input value={name} onChange={(e) => setName(e.target.value)} /></label>
          <label className="field"><span>目标</span><input value={target} onChange={(e) => setTarget(e.target.value)}
            placeholder="1.1.1.1 / host:port / https://example.com" /></label>
          <label className="field"><span>协议</span>
            <select value={protocol} onChange={(e) => setProtocol(e.target.value)}>
              <option value="icmp">ICMP</option><option value="tcp">TCP</option><option value="http">HTTP HEAD</option>
            </select></label>
          <label className="field"><span>IP 版本</span>
            <select value={ipVersion} onChange={(e) => setIpVersion(e.target.value)}>
              <option value="4">IPv4</option><option value="6">IPv6</option>
            </select></label>
          <div className="form-action"><button className="button primary" disabled={busy || !name || !target}
            onClick={() => void add()}><IconPlus size={16} /> 添加</button></div>
        </div>
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>目标列表</h2><p>{targets.length} / 64 个目标</p></div></div>
        <div className="table-wrap flat">
          <table className="server-table">
            <thead><tr><th>名称</th><th>目标</th><th>协议</th><th>状态</th><th></th></tr></thead>
            <tbody>
              {targets.map((t) => (
                <tr key={t.id}>
                  <td><strong>{t.name}</strong>{t.is_default ? <span className="badge-inline">默认</span> : null}</td>
                  <td className="mono ellipsis">{t.target}</td>
                  <td>{t.protocol.toUpperCase()} · IPv{t.ip_version}</td>
                  <td><button className={`status-badge ${t.enabled ? 'online' : ''}`} onClick={() => void toggle(t)}>
                    {t.enabled ? '启用' : '停用'}</button></td>
                  <td>{t.is_default ? null : <button className="icon-button danger" onClick={() => void remove(t.id)} title="删除"><IconX size={15} /></button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!targets.length ? <div className="chart-empty">还没有探测目标</div> : null}
        </div>
      </section>
    </>
  )
}

function Notify({ channels, reload, notify }: {
  channels: NotifyChannel[]
  reload: () => Promise<void>
  notify: (text: string, ok?: boolean) => void
}) {
  const [name, setName] = useState('Telegram')
  const [type, setType] = useState('telegram')
  const [fields, setFields] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const field = (key: string, label: string, placeholder = '') => (
    <label className="field"><span>{label}</span>
      <input value={fields[key] ?? ''} placeholder={placeholder}
        onChange={(e) => setFields({ ...fields, [key]: e.target.value })} /></label>
  )

  const add = async () => {
    setBusy(true)
    try {
      const config: Record<string, unknown> = {}
      for (const [k, v] of Object.entries(fields)) if (v.trim()) config[k] = v.trim()
      if (type === 'email' && config.port) config.port = Number(config.port)
      await api.createChannel({ name, type, config })
      await reload()
      notify('通知渠道已创建')
    } catch (err) { notify(err instanceof Error ? err.message : '创建失败', false) }
    finally { setBusy(false) }
  }
  const remove = async (id: number) => {
    try { await api.deleteChannel(id); await reload(); notify('渠道已删除') }
    catch (err) { notify(err instanceof Error ? err.message : '删除失败', false) }
  }
  const test = async (id: number) => {
    try { await api.testChannel(id); notify('测试通知已发送') }
    catch (err) { notify(err instanceof Error ? err.message : '发送失败', false) }
  }

  return (
    <>
      <section className="panel">
        <div className="panel-head"><div><h2>新建通知渠道</h2><p>Webhook 会经过 SSRF 过滤：拒绝内网、回环、链路本地和云元数据地址。</p></div></div>
        <div className="form-grid">
          <label className="field"><span>名称</span><input value={name} onChange={(e) => setName(e.target.value)} /></label>
          <label className="field"><span>类型</span>
            <select value={type} onChange={(e) => { setType(e.target.value); setFields({}) }}>
              <option value="telegram">Telegram</option><option value="webhook">Webhook</option><option value="email">邮件 SMTP</option>
            </select></label>
          {type === 'telegram' ? <>{field('bot_token', 'Bot Token')}{field('chat_id', 'Chat ID')}</> : null}
          {type === 'webhook' ? <>{field('url', 'Webhook URL', 'https://example.com/hook')}{field('headers', '自定义头（可选，JSON）')}</> : null}
          {type === 'email' ? <>{field('host', 'SMTP 主机')}{field('port', '端口', '465')}{field('username', '用户名')}{field('password', '密码')}{field('from', '发件人')}{field('to', '收件人')}</> : null}
          <div className="form-action"><button className="button primary" disabled={busy || !name} onClick={() => void add()}><IconPlus size={16} /> 添加</button></div>
        </div>
      </section>
      <section className="panel">
        <div className="panel-head"><div><h2>渠道列表</h2><p>测试按钮会发送一条示例通知。</p></div></div>
        <div className="channel-list">
          {channels.map((c) => (
            <div className="channel-row" key={c.id}>
              <div><strong>{c.name}</strong><span>{c.type}</span></div>
              <div className="channel-actions">
                <button className="button ghost" onClick={() => void test(c.id)}>测试</button>
                <button className="icon-button danger" onClick={() => void remove(c.id)} title="删除"><IconX size={15} /></button>
              </div>
            </div>
          ))}
          {!channels.length ? <div className="chart-empty">还没有通知渠道</div> : null}
        </div>
      </section>
    </>
  )
}

function PublicPage({ settings, reload, notify }: {
  settings: SettingsType
  reload: () => Promise<void>
  notify: (text: string, ok?: boolean) => void
}) {
  const [enabled, setEnabled] = useState(!!settings.public_enabled)
  const [busy, setBusy] = useState(false)
  useEffect(() => { setEnabled(!!settings.public_enabled) }, [settings.public_enabled])
  const shareId = settings.public_share_id || ''
  const base = (settings.public_url || location.origin).replace(/\/$/, '')
  const link = shareId ? `${base}/public/${shareId}` : ''

  const save = async (value: boolean) => {
    setBusy(true)
    try {
      await api.saveSettings({ public_enabled: value })
      setEnabled(value)
      await reload()
      notify(value ? '公开页已启用' : '公开页已关闭')
    } catch (err) { notify(err instanceof Error ? err.message : '保存失败', false) }
    finally { setBusy(false) }
  }

  return (
    <section className="panel">
      <div className="panel-head"><div><h2>公开状态页</h2><p>只展示白名单字段：主机名、在线状态、CPU/内存/磁盘、延迟。不展示 IP、Token、配置。</p></div>
        <span className={`status-badge ${enabled ? 'online' : ''}`}>{enabled ? '已启用' : '未启用'}</span></div>
      <div className="inline-form">
        <button className={`button ${enabled ? 'danger' : 'primary'}`} disabled={busy}
          onClick={() => void save(!enabled)}>{enabled ? '关闭公开页' : '启用公开页'}</button>
      </div>
      {enabled && link ? (
        <div className="code-box"><span>公开链接</span><strong className="ellipsis">{link}</strong>
          <button className="button ghost" onClick={() => void navigator.clipboard.writeText(link)}><IconCopy size={15} /> 复制</button></div>
      ) : null}
      <p className="field-hint">公开页无需登录，但只能读取白名单字段；分享 ID 不可枚举，关闭后立即失效。</p>
    </section>
  )
}

function Audit({ events }: { events: AuditEntry[] }) {
  return (
    <section className="panel">
      <div className="panel-head"><div><h2>审计日志</h2><p>最近 100 条管理员与 Agent 关键操作；不记录 Token、密码或会话内容。</p></div></div>
      <div className="table-wrap flat">
        <table className="server-table">
          <thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>目标</th><th>详情</th></tr></thead>
          <tbody>
            {events.map((e) => (
              <tr key={e.id}>
                <td>{clock(Math.floor(new Date(e.timestamp).getTime() / 1000))}</td>
                <td>{e.actor || '—'}</td>
                <td className="mono">{e.action}</td>
                <td>{e.target || '—'}</td>
                <td className="ellipsis">{e.detail || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!events.length ? <div className="chart-empty">还没有审计记录</div> : null}
      </div>
    </section>
  )
}

function About({ version }: { version?: string }) {
  return (
    <section className="panel about-panel">
      <h2>XProbe</h2>
      <p>安全优先的只读服务器探针。Agent 只采集和上报，主控没有远程命令、终端或文件管理功能。</p>
      <ul>
        <li>Go 单二进制 · SQLite · WebSocket</li>
        <li>Agent Token / 注册码 / 管理会话全部哈希存储</li>
        <li>公网 TLS；Cloudflare Tunnel/橙云可选</li>
        <li>内置 CSS 主题，无第三方运行时 JS</li>
      </ul>
      <p className="muted">Version {version ?? '0.3.0'}</p>
    </section>
  )
}
