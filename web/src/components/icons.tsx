/** Small inline icon set (stroke icons, currentColor). */
import type { SVGProps } from 'react'

type P = SVGProps<SVGSVGElement>
const base = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
}

export const IconTrend = (p: P) => (
  <svg {...base} {...p}>
    <path d="M3 17l6-6 4 4 8-8" />
    <path d="M14 7h7v7" />
  </svg>
)
export const IconShield = (p: P) => (
  <svg {...base} {...p}>
    <path d="M12 3l8 3v6c0 4.5-3.4 8.3-8 9-4.6-.7-8-4.5-8-9V6l8-3z" />
    <path d="M9 12l2 2 4-4" />
  </svg>
)
export const IconFlask = (p: P) => (
  <svg {...base} {...p}>
    <path d="M9 3h6M10 3v6L4.5 18.5A1.7 1.7 0 006 21h12a1.7 1.7 0 001.5-2.5L14 9V3" />
    <path d="M7 15h10" />
  </svg>
)
export const IconSun = (p: P) => (
  <svg {...base} {...p}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </svg>
)
export const IconMoon = (p: P) => (
  <svg {...base} {...p}>
    <path d="M20 14.5A8 8 0 019.5 4a8 8 0 1010.5 10.5z" />
  </svg>
)
export const IconGrid = (p: P) => (
  <svg {...base} {...p}>
    <rect x="3" y="3" width="18" height="18" rx="2" />
    <path d="M3 9h18M3 15h18M9 3v18M15 3v18" />
  </svg>
)
export const IconPulse = (p: P) => (
  <svg {...base} {...p}>
    <path d="M3 12h4l3-8 4 16 3-8h4" />
  </svg>
)
export const IconCandles = (p: P) => (
  <svg {...base} {...p}>
    <path d="M7 4v3M7 17v3M17 4v6M17 20v-3" />
    <rect x="5" y="7" width="4" height="10" rx="1" />
    <rect x="15" y="10" width="4" height="7" rx="1" />
  </svg>
)
export const IconAlert = (p: P) => (
  <svg {...base} {...p}>
    <path d="M12 3l9.5 16.5h-19L12 3z" />
    <path d="M12 10v4M12 17.5v.01" />
  </svg>
)
export const IconInfo = (p: P) => (
  <svg {...base} {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 11v5M12 8v.01" />
  </svg>
)
export const IconBlock = (p: P) => (
  <svg {...base} {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M5.6 5.6l12.8 12.8" />
  </svg>
)
export const IconCheck = (p: P) => (
  <svg {...base} {...p}>
    <circle cx="12" cy="12" r="9" />
    <path d="M8 12l3 3 5-6" />
  </svg>
)
export const IconArrowUp = (p: P) => (
  <svg {...base} {...p}>
    <path d="M12 19V5M5 12l7-7 7 7" />
  </svg>
)
export const IconMinus = (p: P) => (
  <svg {...base} {...p}>
    <path d="M5 12h14" />
  </svg>
)
export const IconArrowRight = (p: P) => (
  <svg {...base} {...p}>
    <path d="M5 12h14M13 6l6 6-6 6" />
  </svg>
)
export const IconRefresh = (p: P) => (
  <svg {...base} {...p}>
    <path d="M20 11a8 8 0 10-2.3 5.7" />
    <path d="M20 4v7h-7" />
  </svg>
)
export const IconExternal = (p: P) => (
  <svg {...base} {...p}>
    <path d="M14 4h6v6M20 4l-9 9" />
    <path d="M18 14v5a1 1 0 01-1 1H5a1 1 0 01-1-1V7a1 1 0 011-1h5" />
  </svg>
)
export const IconLogo = (p: P) => (
  <svg viewBox="0 0 24 24" fill="none" aria-hidden {...p}>
    <path d="M4 16l5-5 3.5 3.5L20 7" stroke="white" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    <circle cx="20" cy="7" r="1.8" fill="white" />
  </svg>
)
