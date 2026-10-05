import { useEffect } from 'react'

/** Sets document.title and the meta description for the current page. */
export function usePageTitle(title: string, description?: string) {
  useEffect(() => {
    document.title = `${title} · Trading Bot Console`
    if (description) {
      document.querySelector('meta[name="description"]')?.setAttribute('content', description)
    }
  }, [title, description])
}
