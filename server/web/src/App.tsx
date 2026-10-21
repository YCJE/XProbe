import { useCallback, useEffect, useState } from 'react'
import { api, ApiError, setUnauthorizedHandler } from './api'
import { IconLogo } from './components/Icons'
import { Dashboard } from './pages/Dashboard'
import { Login } from './pages/Login'
import { Public } from './pages/Public'
import { ServerDetail } from './pages/ServerDetail'
import { Settings } from './pages/Settings'
import { Setup } from './pages/Setup'
import type { Session } from './types'

function currentPath(): string {
  return window.location.pathname.replace(/\/+$/, '') || '/'
}

export default function App() {
  const [path, setPath] = useState(currentPath)
  const [session, setSession] = useState<Session | null>(null)
  const [checking, setChecking] = useState(true)
  const [needsSetup, setNeedsSetup] = useState(false)

  const navigate = useCallback((next: string) => {
    if (next === currentPath()) return
    window.history.pushState({}, '', next)
    setPath(currentPath())
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }, [])

  useEffect(() => {
    const onPop = () => setPath(currentPath())
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  useEffect(() => {
    let alive = true
    const boot = async () => {
      // Public share pages are unauthenticated; probing /me here would turn a
      // normal anonymous visit into a 401 redirect to the login form.
      if (currentPath().startsWith('/public/')) {
        if (alive) setChecking(false)
        return
      }
      try {
        const status = await api.setupStatus()
        if (!alive) return
        setNeedsSetup(status.needs_setup)
        if (status.needs_setup) {
          if (currentPath() !== '/setup' && !currentPath().startsWith('/public/')) navigate('/setup')
          return
        }
        try {
          const me = await api.me()
          if (!alive) return
          setSession(me)
          if (currentPath() === '/login' || currentPath() === '/setup') navigate('/')
        } catch {
          if (!alive) return
          setSession(null)
          const p = currentPath()
          if (p !== '/login' && p !== '/setup' && !p.startsWith('/public/')) navigate('/login')
        }
      } finally {
        if (alive) setChecking(false)
      }
    }
    void boot()
    return () => { alive = false }
  }, [navigate])

  const onUnauthorized = useCallback(() => {
    setSession(null)
    // Never bounce a public-page viewer to the admin login because an old
    // protected request failed in the background.
    if (currentPath().startsWith('/public/')) return
    navigate('/login')
  }, [navigate])

  const onLogout = useCallback(() => {
    setSession(null)
    navigate('/login')
  }, [navigate])

  useEffect(() => {
    setUnauthorizedHandler(onUnauthorized)
    return () => setUnauthorizedHandler(null)
  }, [onUnauthorized])

  // The login form only makes sense without a session; history navigation can
  // otherwise land an authenticated admin back on it.
  useEffect(() => {
    if (session && path === '/login') navigate('/')
  }, [session, path, navigate])

  // Returning to the tab revalidates the cookie, closing the window in which
  // a revoked session could still show already-loaded data.
  useEffect(() => {
    if (!session || path.startsWith('/public/')) return
    let inFlight = false
    const check = async () => {
      if (inFlight || document.visibilityState !== 'visible') return
      inFlight = true
      try {
        await api.me()
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) onUnauthorized()
      } finally {
        inFlight = false
      }
    }
    const onVisible = () => { void check() }
    window.addEventListener('focus', onVisible)
    document.addEventListener('visibilitychange', onVisible)
    const timer = window.setInterval(onVisible, 5 * 60 * 1000)
    return () => {
      window.removeEventListener('focus', onVisible)
      document.removeEventListener('visibilitychange', onVisible)
      window.clearInterval(timer)
    }
  }, [session, path, onUnauthorized])

  if (path.startsWith('/public/')) {
    return <Public shareId={path.slice('/public/'.length)} />
  }
  if (checking) {
    return <div className="boot"><div className="boot-mark"><IconLogo size={36} /></div><span>XProbe</span></div>
  }
  if (needsSetup) {
    return <Setup onDone={() => { setNeedsSetup(false); navigate('/login') }} />
  }
  if (path === '/setup') {
    return <Setup onDone={() => navigate('/login')} />
  }
  if (path === '/login' || !session) {
    return <Login onSuccess={(s) => { setSession(s); navigate('/') }} />
  }
  if (path === '/') {
    return <Dashboard session={session} navigate={navigate} onLogout={onLogout} onUnauthorized={onUnauthorized} />
  }
  const detail = path.match(/^\/servers\/(\d+)$/)
  if (detail) {
    return <ServerDetail id={Number(detail[1])} session={session} navigate={navigate}
      onLogout={onLogout} onUnauthorized={onUnauthorized} />
  }
  if (path === '/settings') {
    return <Settings session={session} navigate={navigate} onLogout={onLogout} onUnauthorized={onUnauthorized} />
  }
  return <Dashboard session={session} navigate={navigate} onLogout={onLogout} onUnauthorized={onUnauthorized} />
}
