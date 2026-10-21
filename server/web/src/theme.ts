import { useEffect, useState } from 'react'

export type ThemeMode = 'dark' | 'light' | 'system'
export type ThemeGroup = 'design' | 'classic'
export type ThemeName =
  | 'mission' | 'editorial' | 'graphite'
  | 'glass' | 'classic' | 'minimal' | 'midnight'

export type ThemeDef = {
  id: ThemeName
  label: string
  group: ThemeGroup
  preferredMode?: ThemeMode
}

export const THEMES: readonly ThemeDef[] = [
  { id: 'mission', label: '任务控制', group: 'design', preferredMode: 'dark' },
  { id: 'editorial', label: '纸面编辑', group: 'design', preferredMode: 'light' },
  { id: 'graphite', label: '石墨琥珀', group: 'design', preferredMode: 'dark' },
  { id: 'glass', label: '玻璃拟态', group: 'classic' },
  { id: 'classic', label: '经典实底', group: 'classic' },
  { id: 'minimal', label: '极简描边', group: 'classic' },
  { id: 'midnight', label: '深色高对比', group: 'classic' },
]

const THEME_KEY = 'xprobe.theme'
const MODE_KEY = 'xprobe.mode'
const THEME_IDS = THEMES.map((t) => t.id)

function stored<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  try {
    const value = localStorage.getItem(key) as T | null
    return value && allowed.includes(value) ? value : fallback
  } catch {
    return fallback
  }
}

export function applyTheme(theme: ThemeName, mode: ThemeMode) {
  const root = document.documentElement
  root.dataset.theme = theme
  const resolved = mode === 'system'
    ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark')
    : mode
  root.dataset.mode = resolved
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) {
    // Read the resolved token so every theme gets the right browser chrome.
    const bg = getComputedStyle(root).getPropertyValue('--bg').trim()
    meta.setAttribute('content', bg || (resolved === 'light' ? '#f4f0e8' : '#141412'))
  }
}

// Apply the persisted theme before React mounts so login/setup/public pages do
// not flash the default palette on a fresh load.
export function initTheme() {
  const theme = stored(THEME_KEY, 'graphite', THEME_IDS)
  const mode = stored(MODE_KEY, 'dark', ['dark', 'light', 'system'] as const)
  applyTheme(theme, mode)
}

export function useTheme() {
  const [theme, setThemeState] = useState<ThemeName>(() =>
    stored(THEME_KEY, 'graphite', THEME_IDS))
  const [mode, setModeState] = useState<ThemeMode>(() =>
    stored(MODE_KEY, 'dark', ['dark', 'light', 'system'] as const))

  useEffect(() => {
    applyTheme(theme, mode)
    try {
      localStorage.setItem(THEME_KEY, theme)
      localStorage.setItem(MODE_KEY, mode)
    } catch { /* storage can be unavailable in private browsing */ }
    if (mode !== 'system') return
    const media = matchMedia('(prefers-color-scheme: light)')
    const onChange = () => applyTheme(theme, 'system')
    media.addEventListener('change', onChange)
    return () => media.removeEventListener('change', onChange)
  }, [theme, mode])

  const setTheme = (value: ThemeName) => {
    setThemeState(value)
    const preferred = THEMES.find((t) => t.id === value)?.preferredMode
    if (preferred) setModeState(preferred)
  }

  return {
    theme, mode,
    setTheme,
    setMode: (value: ThemeMode) => setModeState(value),
  }
}
