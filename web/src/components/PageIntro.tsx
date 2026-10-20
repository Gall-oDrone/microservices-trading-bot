/**
 * A page's introductory paragraph (U3, 2026-10-20). Full on desktop; on a
 * phone it is clamped to two lines with a "more / less" toggle, so the first
 * screen shows data, not prose. Same markup everywhere, the clamp is CSS.
 */
import { useState, type ReactNode } from 'react'

export function PageIntro({ children, id }: { children: ReactNode; id?: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div className={`page-intro ${open ? 'open' : ''}`} id={id} data-testid="page-intro">
      <p>{children}</p>
      <button
        type="button"
        className="page-intro-toggle"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        data-testid="page-intro-toggle"
      >
        {open ? 'less' : 'more'}
      </button>
    </div>
  )
}
