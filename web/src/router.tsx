import { QueryClient } from '@tanstack/react-query'
import type { RouteObject } from 'react-router'
import { AppShell } from './components/AppShell'
import { ForwardTestDetailPage } from './pages/ForwardTestDetailPage'
import { ForwardTestsPage } from './pages/ForwardTestsPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RiskPage } from './pages/RiskPage'

export const routes: RouteObject[] = [
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <ForwardTestsPage /> },
      { path: 'forward-tests/:book', element: <ForwardTestDetailPage /> },
      { path: 'risk', element: <RiskPage /> },
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
