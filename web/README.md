# web: operator console

React 19 + TypeScript (strict) + Vite. Reads only from [`services/ui-api`](../services/ui-api).
Plan: [`docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md`](../docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md).

Node is not installed system-wide on the dev box; a portable Node 22 lives in `.tools/node`
(gitignored):

```bash
export PATH=$PWD/.tools/node/bin:$PATH      # from the repo root
cd web && npm ci
```

## Run

```bash
# 1. backend (another terminal)
cd services/ui-api && go run ./cmd
# 2. UI on http://127.0.0.1:5173, /api proxied to 127.0.0.1:8090
cd web && npm run dev

# Without the backend: MSW serves the captured fixtures
npm run dev:mock
```

## Check

```bash
npm run ci        # typecheck, lint, prettier check, vitest, production build
npm run fixtures  # re-capture src/mocks/fixtures from a running ui-api
```

## Layout

| Path | What |
|---|---|
| `src/api/schemas.ts` | zod schemas mirroring the Go view models; every response is validated |
| `src/api/client.ts` | fetch wrapper and TanStack Query hooks (60 s refresh) |
| `src/pages/` | Forward tests, forward-test detail, Risk |
| `src/components/` | shell, charts (Lightweight Charts), UI primitives, icons |
| `src/index.css` | design tokens: long = teal, flat = slate, loss = coral, warn = amber, benchmark = grey dashed |
| `src/mocks/` | MSW handlers and fixtures captured from the real ui-api |
| `src/test/` | contract tests (fixtures vs schemas) and page tests |
