import { QueryClient } from '@tanstack/react-query'
import type { RouteObject } from 'react-router'
import { AppShell } from './components/AppShell'
import { ForwardTestDetailPage } from './pages/ForwardTestDetailPage'
import { ForwardTestsPage } from './pages/ForwardTestsPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { ResearchPage } from './pages/ResearchPage'
import { RiskPage } from './pages/RiskPage'
import { RunPage } from './pages/RunPage'
import { RunsPage } from './pages/RunsPage'
import { StudyPage } from './pages/StudyPage'

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <ForwardTestsPage /> },
      { path: 'forward-tests/:book', element: <ForwardTestDetailPage /> },
      { path: 'risk', element: <RiskPage /> },
      { path: 'research', element: <ResearchPage /> },
      { path: 'research/runs', element: <RunsPage /> },
      { path: 'research/runs/:date/:name', element: <RunPage /> },
      { path: 'research/:name', element: <StudyPage /> },
      { path: '*', element: <NotFoundPage /> },
    ],
  },
]

export function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: 1, refetchOnWindowFocus: true, staleTime: 15_000 },
    },
  })
}
