import { useEffect } from 'react'
import { useEnvironment } from '../api/client'
import { faviconFor, titleFor } from './env'

/**
 * Sets document.title and the meta description for the current page. On the
 * REAL account (the measurement canary's ledger, 2026-10-22) the title is
 * prefixed `REAL ·` and the favicon gets a red dot, so a tab on the real
 * console is told apart from a demo tab before it is even focused.
 */
export function usePageTitle(title: string, description?: string) {
  const env = useEnvironment()
  useEffect(() => {
    document.title = titleFor(title, env)
    if (description) {
      document.querySelector('meta[name="description"]')?.setAttribute('content', description)
    }
    document.querySelector('link[rel="icon"]')?.setAttribute('href', faviconFor(env))
  }, [title, description, env])
}
