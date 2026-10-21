import { useState } from 'react'
import { api, ApiError } from '../api'
import type { Session } from '../types'
import { IconLogo, IconShield } from '../components/Icons'

type Props = {
  onSuccess: (session: Session) => void
}

export function Login({ onSuccess }: Props) {
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [showTotp, setShowTotp] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setError('')
    setBusy(true)
    try {
      const session = await api.login(username.trim(), password, showTotp ? totp : undefined)
      onSuccess(session)
    } catch (err) {
      const message = err instanceof ApiError ? err.message : '无法连接服务器'
      setError(message)
      if (message.includes('两步验证')) setShowTotp(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="auth-shell">
      <div className="auth-visual">
        <div className="auth-brand"><IconLogo size={26} /> XProbe</div>
        <h1>你的服务器，<br /><span>只看不改。</span></h1>
        <p>只读探针。Agent 只上报，主控不下发命令，没有 WebSSH、文件管理或远程执行。</p>
        <ul className="auth-points">
          <li><IconShield size={16} /> 无控制通道，从架构上消除 RCE</li>
          <li><IconShield size={16} /> Token 哈希存储，强制公网 TLS</li>
          <li><IconShield size={16} /> 单管理员，默认无弱口令</li>
        </ul>
        <div className="auth-orbit" aria-hidden="true">
          <span /><span /><span />
        </div>
      </div>
      <div className="auth-panel">
        <form className="auth-card" onSubmit={submit}>
          <div className="auth-card-head">
            <h2>登录控制台</h2>
            <p>使用管理员账号进入只读监控面板。</p>
          </div>
          <label className="field">
            <span>用户名</span>
            <input value={username} onChange={(e) => setUsername(e.target.value)}
              autoComplete="username" autoFocus required />
          </label>
          <label className="field">
            <span>密码</span>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password" required />
          </label>
          {showTotp ? (
            <label className="field">
              <span>两步验证码</span>
              <input value={totp} onChange={(e) => setTotp(e.target.value.toUpperCase().slice(0, 20))}
                autoComplete="one-time-code" placeholder="6 位验证码或 20 位恢复码" />
            </label>
          ) : (
            <button type="button" className="text-button" onClick={() => setShowTotp(true)}>
              使用两步验证码
            </button>
          )}
          {error ? <div className="form-error" role="alert">{error}</div> : null}
          <button className="button primary wide" disabled={busy} type="submit">
            {busy ? '验证中…' : '登录'}
          </button>
          <p className="auth-footnote">首次使用？请先完成 <code>/setup</code> 初始化。</p>
        </form>
      </div>
    </div>
  )
}
