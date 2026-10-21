import type { ReactNode } from 'react'
import { api } from '../api'
import type { Session } from '../types'
import { THEMES, useTheme } from '../theme'
import { IconDashboard, IconLogo, IconLogout, IconMoon, IconMonitor, IconSettings, IconSun } from './Icons'

type Props = {
  session: Session
  active: 'dashboard' | 'settings'
  navigate: (path: string) => void
  onLogout: () => void
  children: ReactNode
}

export function Layout({ session, active, navigate, onLogout, children }: Props) {
  const { theme, mode, setTheme, setMode } = useTheme()
  const cycleMode = () => setMode(mode === 'dark' ? 'light' : mode === 'light' ? 'system' : 'dark')
  const ModeIcon = mode === 'light' ? IconSun : mode === 'system' ? IconMonitor : IconMoon

  const logout = async () => {
    try { await api.logout() } catch { /* the session is gone either way */ }
    onLogout()
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar-inner">
          <button className="brand" onClick={() => navigate('/')} aria-label="XProbe 首页">
            <span className="brand-mark"><IconLogo size={22} /></span>
            <span className="brand-name">XProbe</span>
            <span className="brand-pill">READ ONLY</span>
          </button>
          <nav className="nav" aria-label="主导航">
            <button className={`nav-link ${active === 'dashboard' ? 'active' : ''}`} onClick={() => navigate('/')}>
              <IconDashboard size={17} /> 概览
            </button>
            <button className={`nav-link ${active === 'settings' ? 'active' : ''}`} onClick={() => navigate('/settings')}>
              <IconSettings size={17} /> 设置
            </button>
          </nav>
          <div className="topbar-actions">
            <select className="theme-select" value={theme} onChange={(e) => setTheme(e.target.value as typeof theme)}
              aria-label="主题">
              <optgroup label="新版主题">
                {THEMES.filter((t) => t.group === 'design').map((t) => (
                  <option value={t.id} key={t.id}>{t.label}</option>
                ))}
              </optgroup>
              <optgroup label="经典主题">
                {THEMES.filter((t) => t.group === 'classic').map((t) => (
                  <option value={t.id} key={t.id}>{t.label}</option>
                ))}
              </optgroup>
            </select>
            <button className="icon-button" onClick={cycleMode} title={`外观：${mode}`} aria-label="切换外观">
              <ModeIcon size={18} />
            </button>
            <span className="user-chip" title={session.username}>{session.username}</span>
            <button className="icon-button" onClick={logout} title="退出登录" aria-label="退出登录">
              <IconLogout size={18} />
            </button>
          </div>
        </div>
      </header>
      <main className="page">{children}</main>
      <footer className="footer">XProbe · 只读服务器状态 · Agent 永不接受远程命令</footer>
    </div>
  )
}
