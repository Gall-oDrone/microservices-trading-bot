/**
 * Light / dark theme. Dark is the default; the choice is stored in
 * localStorage and applied as `data-theme` on <html>. index.html applies the
 * saved theme before first paint so there is no flash.
 */
import { useSyncExternalStore } from 'react'

export type Theme = 'dark' | 'light'

export const THEME_KEY = 'theme'
const META: Record<Theme, string> = { dark: '#0b0f17', light: '#f5f7fa' }
const listeners = new Set<() => void>()

function read(): Theme {
  try {
    return localStorage.getItem(THEME_KEY) === 'light' ? 'light' : 'dark'
  } catch {
    return 'dark'
  }
}

let current: Theme = typeof window === 'undefined' ? 'dark' : read()

/** Writes the theme to the document (data-theme + theme-color meta). */
export function applyTheme(theme: Theme) {
  if (typeof document === 'undefined') return
  document.documentElement.dataset.theme = theme
  document.documentElement.style.colorScheme = theme
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', META[theme])
}

export function getTheme(): Theme {
  return current
}

export function setTheme(theme: Theme) {
  current = theme
  try {
    localStorage.setItem(THEME_KEY, theme)
  } catch {
    // Private mode: the choice lasts for this page only.
  }
  const root = document.documentElement
  const smooth = !window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
  if (smooth) {
    root.classList.add('theme-switching')
    window.setTimeout(() => root.classList.remove('theme-switching'), 320)
  }
  applyTheme(theme)
  listeners.forEach((l) => l())
}

function subscribe(l: () => void) {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

/** Current theme; re-renders on change (charts use it to rebuild with new colours). */
export function useTheme(): [Theme, (t: Theme) => void] {
  const theme = useSyncExternalStore(subscribe, getTheme, () => 'dark' as Theme)
  return [theme, setTheme]
}

// Test hook: re-read storage (vitest resets localStorage between tests).
export function resetThemeForTests() {
  current = read()
  applyTheme(current)
}
