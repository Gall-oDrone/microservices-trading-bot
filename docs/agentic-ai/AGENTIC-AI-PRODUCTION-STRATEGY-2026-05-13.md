# Agentic AI — Production Strategy: Reconciliation, Reporting, and Research Agents

Date: 2026-05-13  
Repository: `microservices-trading-bot`

## 1) Objective

Document the **production-optimal** way to use agentic AI in this platform when the work touches **profit and loss**, **exchange fills**, **internal telemetry**, and **operator-facing summaries**. This complements the implementation roadmap in `docs/agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md` by answering:

- Where **deterministic code** must own truth versus where **LLM agents** add value.
- How **`paper-trading-reporter`** snapshots relate (and do not relate) to **Bitso execution data**.
- How **multi-agent research frameworks** (for example [TradingAgents](https://github.com/Gall-oDrone/TradingAgents.git)) fit without compromising production safety.

---

## 2) Definitions

| Term | Meaning in this document |
|------|---------------------------|
| **Canonical execution data** | Fills, fees, order IDs, and venue timestamps ingested via the platform’s own sync path (for example `services/order-management` and `shared/pkg/bitso`), stored durably for replay and audit. |
| **Reconciliation artifact** | Structured JSON (or equivalent) produced by **tested** batch or streaming jobs: computed PnL components, fee totals, position deltas, and **machine-readable mismatches** versus internal expectations. |
| **Paper trading snapshot** | JSON emitted by `services/paper-trading-reporter`: strategy registry payloads and per-book indicator snapshots from `strategy-executor` (`docs/agentic-ai/…` and `services/paper-trading-reporter/internal/models/snapshot.go`). |
| **Agent lane** | `ops-agent`, future specialized HTTP agents, and `agent-coordinator` fan-out: read-heavy, policy-bounded, **narrative and investigation** on top of artifacts. |
| **Cold path** | Scheduled or human-triggered analysis (research memos, post-mortems) with **no** direct live order placement unless explicit gates exist. |

---

## 3) Guiding Principles (Production)

1. **Deterministic systems own money, risk, and truth.** PnL arithmetic, fee aggregation, position, and venue reconciliation belong in **code** (Go services, SQL, batch jobs), not in model reasoning.
2. **Agents own language, synthesis, and guided investigation** on **structured inputs** they did not compute: reconciliation JSON, metrics, health checks, deployment metadata.
3. **Single pipeline to the venue for canonical facts.** Avoid several autonomous agents each calling Bitso ad hoc; prefer one sync/reconciliation writer and **read-only** tools that fetch **stored** snapshots by ID.
4. **Schema alignment before “compare.”** Two JSON blobs are comparable in production only when they share a **versioned contract** (fields, units, time basis). Otherwise comparisons belong in **tests**, not in free-form LLM diffing.
5. **CSV is a convenience, not the spine.** Exports for humans or ad hoc tools are fine; **primary** reconciliation should use API-backed or DB-backed canonical data with pagination, retries, and idempotency.
6. **Research-style multi-agent trading stays off the hot path.** Frameworks inspired by multi-agent “trading desk” simulations belong in **paper**, **shadow**, or **offline** workflows with human or rule gates before any connection to live orders.

These align with the safety posture already stated in `AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md` (read-only first, auditability, budgets).

---

## 4) Repository Context (What Each Piece Is For)

### 4.1 `services/paper-trading-reporter`

- Collects from **`strategy-executor`**: `GET /api/v1/strategies` and `GET /api/v1/indicators/{book}/snapshot`.
- Produces `PaperTradingSnapshot` (schema version, strategies, indicators, collection errors) and uploads JSON to S3.
- **It is not** a substitute for a **fill ledger** or **exchange PnL** unless extended with explicit, versioned execution fields aligned to internal orders.

### 4.2 `services/order-management` and Bitso

- Venue integration and sync (including user trades polling) are the natural home for **authoritative** execution-side data **inside** the platform boundary.
- Production reconciliation should **prefer** persisted data the platform already trusts, over manual CSV drops.

### 4.3 `services/ops-agent` and `services/agent-coordinator`

- **Ops-agent:** incident-oriented triage with **allowlisted tools** (for example Prometheus query, HTTP health) and optional LLM summarization (`services/ops-agent/internal/agent/ops_agent.go`).
- **Agent-coordinator:** fans out to child agents over HTTP and aggregates recommendations (`services/agent-coordinator/internal/coordinator/coordinator.go`).
- Both are **operationally** oriented today; **financial reconciliation** should follow the same pattern: **tools fetch artifacts**, models **explain** and **route** operators, not **derive** balances.

---

## 5) Recommended Architecture: Two Layers

### Layer A — Deterministic reconciliation (required for production)

**Inputs:** Canonical fills/fees/orders from the platform’s Bitso sync (or a controlled batch pull with cursor and retries).  
**Outputs:** `reconciliation_report.json` (or rows) with:

- Run ID, time window, book, schema version.
- Aggregates: fees, notional, realized/unrealized splits as defined by your accounting model.
- **Deltas** versus internal expectations when those exist (strategy-reported vs OM state, etc.).
- **No** natural language in the authoritative record (optional `notes` field populated by humans only).

**Properties:** Unit-tested, replayable, suitable for CI and alerts (for example “delta exceeds threshold”).

### Layer B — Agentic interpretation (optional but high leverage)

**Inputs:** Latest reconciliation artifact by ID, plus existing ops signals (Prometheus, health, deploy).  
**Outputs:** Operator summary: what diverged, likely classes of causes (with **confidence**), suggested **next checks**, links to runbooks.

**Implementation sketch (consistent with this repo):**

- Add a **read-only tool** to `ops-agent` (or a sibling `forensics-agent`): `fetch_reconciliation_report(run_id)` from S3 or an internal API.
- Register a **child client** on `agent-coordinator` if multiple evidence sources should run in parallel (metrics + reconciliation + Kafka health).
- Keep **policy allowlists and budgets** (`shared/pkg/agent`) as the hard gate.

---

## 6) Comparing “Latest Trades” to `paper-trading-reporter` JSON

**Production rule:** Do not ask an agent to “compare CSV to snapshot” without a **shared schema**.

**Preferred sequence:**

1. **Normalize** Bitso-side trades into the same logical model as internal fills (IDs, fees, timestamps in UTC, major/minor, book).
2. **Emit** a reconciliation artifact that references **both** sides only after field mapping is defined in code.
3. **Optionally** pass the **diff section** of that artifact to an agent for **narrative** and **runbook** alignment.

If the goal is “does our bot’s view of the world match the exchange,” the artifact should compare **OM / trading-engine state** to **canonical Bitso-derived rows**, not raw `PaperTradingSnapshot` indicators—unless you **extend** the snapshot (or add a sibling export) with **explicit join keys** and PnL-relevant fields.

---

## 7) External Research Frameworks (TradingAgents-Style)

Frameworks such as [TradingAgents](https://github.com/Gall-oDrone/TradingAgents.git) (multi-role analysts, LangGraph-style workflows, decision logs) are valuable for:

- **Research memos** and scenario exploration on historical windows.
- **Paper** or **shadow** “desk” narratives that **do not** place orders.

**Production placement:**

- Run as a **separate** batch or service that consumes **exported** snapshots + market data, writes **markdown/PDF/JSON memos** to object storage.
- **Do not** attach directly to the live order path without simulation, rate limits, and **human or policy approval** for any state change.

This repository’s **Go hot path** (strategy-executor → Kafka → trading-engine → order-management) should remain **non-LLM** for determinism and latency.

---

## 8) Phased Roadmap (Execution-Focused)

### Phase 1 — Artifact first (highest leverage)

- [ ] Define a **versioned** `reconciliation_report` JSON schema.
- [ ] Implement the **deterministic** producer (job or service) reading canonical store / sync.
- [ ] Persist artifacts with immutable keys (S3 prefix by date + run ID).

**Exit criteria:** Green unit tests on golden files; alertable thresholds on deltas without any LLM.

### Phase 2 — Ops integration

- [ ] Read-only tool: fetch latest reconciliation summary by `run_id` or “latest for book.”
- [ ] Extend coordinator fan-out if multiple diagnostic agents are needed.
- [ ] LangSmith / trace IDs correlate **agent run** ↔ **reconciliation run**.

**Exit criteria:** On-call can open one coordinated report linking metrics + reconciliation diff excerpt.

### Phase 3 — Optional cold-path research

- [ ] Scheduled export: strategy snapshot + reconciliation + key metrics bundle.
- [ ] External or internal **research** agent consumes bundle; output stored as non-authoritative memo.

**Exit criteria:** Human-readable report; no automated order placement from memo.

---

## 9) Observability and Governance

- **SLOs:** Reconciliation job success rate, lag from last fill to artifact freshness, max delta before page.
- **Agent SLOs:** As in `AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md` (run success, P95 latency, token/cost budgets).
- **Audit:** Store prompts and tool outputs for agent runs that reference financial artifacts; redact secrets and account identifiers.

---

## 10) Anti-Patterns (Explicit)

- Using **LLM arithmetic** for fees, balances, or tax-relevant totals.
- Treating **CSV uploads** as the **primary** reconciliation source of truth.
- **Multiple uncached Bitso callers** inside autonomous agent loops (rate limits, inconsistent snapshots).
- **Extending** `paper-trading-reporter` with prose or LLM output—keep it **machine data**; let agents read it.

---

## 11) Related Documents

- `docs/agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md` — phased agent integration, interfaces, LangChain notes.
- `docs/parallel-strategies/PARALLEL-STRATEGIES-SCALING-PLAN-2026-05-12.md` — scaling and safety constraints for parallel strategies (relevant when agent narratives reference executor topology).
- `docs/PNL-DEBUGGING-AND-FIXES.md` — practical PnL debugging context in this repo.
- `docs/ORDER-FLOW-AND-BITSO-TESTING.md` — order flow and venue testing posture.

---

## 12) Summary

**Production-optimal approach:** implement **canonical execution ingestion + deterministic reconciliation artifacts** first; use **`ops-agent` / `agent-coordinator`** to **interpret** those artifacts alongside existing observability tools; keep **TradingAgents-style** workflows on the **cold path**; align any “compare to reporter JSON” work through **explicit schema evolution**, not ad hoc CSV diffing by models.

This preserves auditability, minimizes venue and cost risk, and matches the architecture already present in `services/ops-agent`, `services/agent-coordinator`, and `services/paper-trading-reporter`.
