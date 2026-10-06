import type { Study, StudyKind } from '../api/schemas'

export const KIND_LABEL: Record<StudyKind, string> = {
  study: 'study',
  preregistration: 'pre-registration',
  report: 'report',
  assessment: 'assessment',
}

export const KIND_TONE: Record<StudyKind, 'info' | 'long' | 'flat' | 'warn'> = {
  study: 'info',
  preregistration: 'long',
  report: 'flat',
  assessment: 'warn',
}

/** Short human name for a study id: "PATH-TO-PROFITABILITY-2026-09-23" -> "Path to profitability". */
export function shortName(name: string): string {
  const s = name
    .replace(/-?\d{4}-\d{2}-\d{2}$/, '')
    .replace(/-/g, ' ')
    .toLowerCase()
  return s.charAt(0).toUpperCase() + s.slice(1)
}

/** Every word of `q` appears in the title, id, summary or question (case-insensitive). */
export function matchesStudy(s: Study, q: string): boolean {
  const words = q.toLowerCase().split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const hay = `${s.title} ${s.name} ${s.summary} ${s.question ?? ''}`.toLowerCase()
  return words.every((w) => hay.includes(w))
}
