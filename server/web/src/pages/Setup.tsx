import { useEffect, useState } from 'react'
import { api, ApiError } from '../api'
import { IconLogo, IconShield } from '../components/Icons'

type Props = { onDone: () => void }

export function Setup({ onDone }: Props) {
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null)
  const [token, setToken] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [done, setDone] = useState(false)

  useEffect(() => {
    api.setupStatus().then((s) => setNeedsSetup(s.needs_setup)).catch(() => setNeedsSetup(true))
  }, [])

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    setError('')
    if (password !== confirm) { setError('两次输入的密码不一致'); return }
    if (password.length < 12) { setError('密码至少 12 位'); return }
    if (!/[A-Z]/.test(password) || !/[a-z]/.test(password) || /\d/.test(password) === false) {
      setError('密码必须包含大写字母、小写字母和数字')
      return
    }
    setBusy(true)
    try {
      await api.setup(token.trim(), password)
      setDone(true)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '无法连接服务器')
    } finally {
      setBusy(false)
    }
  }

  if (done) {
    return (
      <div className="auth-shell single">
        <div className="auth-card success-card">
          <div className="success-mark"><IconShield size={34} /></div>
          <h2>初始化完成</h2>
          <p>管理员账号已创建。现在登录控制台，添加你的第一台服务器。</p>
          <button className="button primary wide" onClick={onDone}>去登录</button>
        </div>
      </div>
    )
  }

  if (needsSetup === false) {
    return (
      <div className="auth-shell single">
        <div className="auth-card success-card">
          <h2>初始化已完成</h2>
          <p>管理员账号已存在，不需要重复初始化。</p>
          <button className="button primary wide" onClick={onDone}>去登录</button>
        </div>
      </div>
    )
  }

  return (
    <div className="auth-shell">
      <div className="auth-visual">
        <div className="auth-brand"><IconLogo size={26} /> XProbe</div>
        <h1>先设一个<br /><span>足够强的密码。</span></h1>
        <p>初始化只做一次。setup token 在服务端启动日志和数据目录的 <code>setup_token</code> 文件中，30 分钟内有效。</p>
        <ul className="auth-points">
          <li><IconShield size={16} /> 无默认密码，无 admin/admin</li>
          <li><IconShield size={16} /> Argon2id 密码哈希</li>
          <li><IconShield size={16} /> 初始化完成后接口永久关闭</li>
        </ul>
        <div className="auth-orbit" aria-hidden="true"><span /><span /><span /></div>
      </div>
      <div className="auth-panel">
        <form className="auth-card" onSubmit={submit}>
          <div className="auth-card-head">
            <h2>初始化管理员</h2>
            <p>输入启动日志中的 setup token，并设置管理员密码。</p>
          </div>
          <label className="field">
            <span>Setup token</span>
            <input value={token} onChange={(e) => setToken(e.target.value)}
              autoComplete="off" spellCheck={false} autoFocus required />
          </label>
          <label className="field">
            <span>管理员密码</span>
            <input type="password" value={password} onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password" required />
          </label>
          <label className="field">
            <span>确认密码</span>
            <input type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password" required />
          </label>
          <p className="field-hint">至少 12 位，包含大写字母、小写字母和数字。</p>
          {error ? <div className="form-error" role="alert">{error}</div> : null}
          <button className="button primary wide" disabled={busy || needsSetup === null} type="submit">
            {busy ? '创建中…' : '创建管理员'}
          </button>
        </form>
      </div>
    </div>
  )
}
