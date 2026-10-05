import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, vi } from 'vitest'
import { handlers } from '../mocks/handlers'

// Lightweight Charts needs a real canvas; tests check data and text, not pixels.
vi.mock('lightweight-charts', () => {
  const series = () => ({
    setData: vi.fn(),
    createPriceLine: vi.fn(),
    priceScale: () => ({ applyOptions: vi.fn() }),
  })
  return {
    createChart: () => ({
      addSeries: vi.fn(series),
      priceScale: () => ({ applyOptions: vi.fn() }),
      timeScale: () => ({ fitContent: vi.fn() }),
      applyOptions: vi.fn(),
      remove: vi.fn(),
    }),
    createSeriesMarkers: vi.fn(),
    CandlestickSeries: {},
    LineSeries: {},
    HistogramSeries: {},
    ColorType: { Solid: 'solid' },
    LineStyle: { Solid: 0, Dotted: 1, Dashed: 2 },
  }
})

export const server = setupServer(...handlers)

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }))
afterEach(() => {
  server.resetHandlers()
  cleanup()
})
afterAll(() => server.close())
