# Agentic AI Integration Plan (Production-Ready)

Date: 2026-05-07  
Repository: `microservices-trading-bot`

## 1) Objective

Introduce agentic AI capabilities safely into the trading platform with:

- Production reliability and controlled autonomy
- Provider-agnostic LLM integration (Anthropic + OpenAI)
- OOP-style interfaces to minimize refactoring across providers
- LangChain integration for orchestration, tools, memory, and evaluation workflows

The primary goal is operational intelligence (incident triage and runbook assistance), not direct autonomous trading decisions in phase 1.

## 2) Guiding Principles

- Safety first: read-only agents before write-capable agents
- Deterministic signals first: use Prometheus/Alertmanager/K8s as source of truth
- Human-in-the-loop for state-changing actions
- Full auditability: prompt, tool calls, decisions, and outcomes
- Strict cost and latency budgets
- Incremental rollout with clear rollback paths

## 3) Scope for Initial Delivery

### In scope

- Create an `ops-agent` service for incident detection/triage assistance
- Consume monitoring events (Alertmanager webhooks and periodic checks)
- Query observability tools (Prometheus, Kubernetes health/logs, service endpoints)
- Produce incident summaries and action recommendations
- Expose API endpoint for manual triggering and retrieval

### Out of scope (phase 1)

- Autonomous production trading decisions
- Unrestricted execution of remediation actions
- Complex multi-agent autonomous loops without human approval gates

## 4) Why not start with a fleet that watches Grafana dashboards?

A dashboard-monitoring fleet is not the best first production step.  
Preferred strategy:

1. Use Prometheus + Alertmanager + API checks as canonical signals.
2. Use Grafana primarily for visualization and post-incident analysis.
3. Add synthetic validation of key dashboard queries (promql checks) instead of UI-level screenshot interpretation in early phases.

This avoids brittle visual checks and improves determinism and explainability.

## 5) Proposed Architecture

## 5.1 High-level components

- `services/ops-agent`
  - Event intake (alerts + schedule)
  - Agent orchestration runtime
  - Policy guardrails (tool allowlist, budget, timeouts)
  - Incident memory (short-lived context)
  - API for manual trigger, status, and reports

- `shared/pkg/agent`
  - Provider-agnostic interfaces and core types
  - Tool interface and execution contracts
  - Policy and budget enforcement helpers
  - Tracing and audit structures

- `shared/pkg/agent/providers`
  - `anthropic` implementation
  - `openai` implementation

- `shared/pkg/agent/tools`
  - `prometheus_query`
  - `k8s_pod_status`
  - `k8s_logs`
  - `http_healthcheck`
  - `runbook_lookup`

- `services/agent-coordinator` (new, not yet implemented)
  - Dispatches incidents to specialized agents
  - Applies global policy and cross-agent budget limits
  - Aggregates sub-agent outputs into a single operator report
  - Handles escalation routing and operator handoff

## 5.2 Data flow

1. Alertmanager event or scheduled checker triggers `ops-agent`.
2. Orchestrator builds context and selects workflow.
3. Agent calls restricted tools through policy layer.
4. Agent emits:
   - root-cause hypothesis,
   - confidence,
   - impacted services,
   - recommended next actions,
   - escalation target.
5. All inputs/outputs are persisted to an audit store and exposed via API.

## 5.3 Coordinator agent design

Coordinator role:

- Entry point for all incident workflows in multi-agent mode
- Selects execution strategy (single-agent fast path vs multi-agent fan-out)
- Calls specialized agents (`ops-agent`, `kafka-agent`, `execution-agent`) using standard `Agent` interface
- Merges hypotheses, scores confidence, and resolves conflicting recommendations
- Emits final incident report with explicit traceability to each sub-agent run

Coordinator constraints:

- Read-only by default in phase 1 and 2
- Requires approval gate before forwarding any write-capable remediation action
- Enforces global run budgets (tokens/cost/time) across all child agents

Status:

- [x] Coordinator service scaffold implemented (`services/agent-coordinator`)
- [x] Planned in architecture and phased roadmap
- [ ] Full LangSmith client integration pending (currently trace interface + noop hook)

## 6) OOP-style Interface Design (Go)

Design interfaces so providers can be swapped with minimal application changes.

```go
type LLMProvider interface {
    Name() string
    Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)
    Stream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error)
}

type Agent interface {
    ID() string
    Run(ctx context.Context, input AgentInput) (AgentResult, error)
}

type Tool interface {
    Name() string
    Description() string
    Run(ctx context.Context, args map[string]any) (ToolResult, error)
}

type PolicyEngine interface {
    ValidateToolCall(ctx context.Context, req ToolCallRequest) error
    ValidateBudget(ctx context.Context, usage UsageSnapshot) error
}
```

### Notes

- Keep provider-specific request/response formats inside provider adapters.
- Use normalized domain types in core agent packages.
- Inject provider and tools via constructors to simplify testing and migration.

## 7) LangChain Integration Plan

Use LangChain as orchestration middleware while keeping business interfaces provider-agnostic.

## 7.1 Recommended usage

- Use LangChain for:
  - Tool calling orchestration
  - Prompt templating
  - Chain/workflow composition
  - Optional retrieval over runbooks/docs
  - Evaluation pipelines and regression checks

- Keep critical controls outside LangChain:
  - Policy enforcement
  - Budget caps
  - Authentication/RBAC
  - Audit persistence

## 7.2 Integration model

- `ops-agent` workflow owns business logic and policy checks.
- LangChain executes composable steps:
  - classify incident
  - gather diagnostics via tools
  - synthesize report
- Provider adapters remain swappable (Anthropic/OpenAI) behind `LLMProvider`.

## 7.3 Language/runtime decision

Two viable options:

1. **Go-first with sidecar orchestrator (recommended initially):**
   - Keep core microservices in Go.
   - Add a Python/Node LangChain sidecar or separate `ops-agent` runtime.
   - Communicate through HTTP/gRPC.
   - Fastest way to leverage mature LangChain ecosystem while isolating risk.

2. **Pure Go implementation with minimal external orchestration layer:**
   - Use direct provider SDKs first.
   - Add LangChain only where high-value (evaluation/retrieval).
   - Lower operational complexity but less out-of-the-box orchestration convenience.

## 7.4 LangChain + LangSmith for coordinator workflows

- Use `langchaingo` agent executor as coordinator workflow engine for:
  - routing to specialized agents/tools
  - iterative planning with bounded loops
  - structured output generation for final incident report
- Use LangSmith client/runs APIs for:
  - end-to-end tracing of coordinator and child-agent runs
  - run-level observability (latency, errors, token usage)
  - evaluation datasets for regression testing coordinator decisions
- Keep policy/RBAC enforcement in Go service layer, outside LangChain runtime.

## 8) Phased Delivery Roadmap

## Phase 0 - Foundations (1 week)

- [x] Define interfaces in `shared/pkg/agent`
- [x] Implement provider adapters skeleton (`anthropic`, `openai`)
- [x] Add policy engine and budget model
- [x] Add feature flags and kill-switch
- [ ] Add audit schema

Exit criteria:

- [x] Unit tests pass for provider contracts and policy checks
- [x] Can switch provider by config only

Status: **Completed (2026-05-07)**  
Notes:
- Implemented in:
  - `shared/pkg/agent/types.go`
  - `shared/pkg/agent/policy.go`
  - `shared/pkg/agent/config.go`
  - `shared/pkg/agent/providers/anthropic/provider.go`
  - `shared/pkg/agent/providers/openai/provider.go`
  - `shared/pkg/agent/policy_test.go`
  - `shared/pkg/agent/config_test.go`
- Verification run:
  - `cd shared && go test ./...` (pass)

## Phase 1 - Read-only Ops Agent (2 weeks)

- [x] Build `services/ops-agent`
- [x] Integrate Alertmanager webhook intake
- [x] Integrate read-only tool adapters
- [x] Generate incident report payloads
- [x] Add metrics (`agent_runs_total`, `agent_run_latency_ms`, `agent_failures_total`)
- [ ] Add persistent audit logs (currently in-memory report store)

Exit criteria:

- [x] Produces initial triage reports with deterministic recommendations
- [x] No write operations enabled
- [ ] Audit logs complete

Status: **In Progress (2026-05-07)**  
Notes:
- Added new service/module:
  - `services/ops-agent/cmd/main.go`
  - `services/ops-agent/internal/config/config.go`
  - `services/ops-agent/internal/server/http_server.go`
  - `services/ops-agent/internal/agent/ops_agent.go`
  - `services/ops-agent/internal/tools/healthcheck.go`
  - `services/ops-agent/internal/tools/prometheus_query.go`
- Added tests:
  - `services/ops-agent/internal/agent/ops_agent_test.go`
  - `services/ops-agent/internal/config/config_test.go`
- Verification run:
  - `cd services/ops-agent && go test ./...` (pass)

## Phase 2 - Assisted Remediation (2 weeks)

- Add action proposals with approval workflow
- Add chat/CLI integration for operator approval
- Implement safe actions (restart deployment, scale within bounds)

Exit criteria:

- Every action requires explicit operator approval
- Policy engine blocks unauthorized actions

## Phase 3 - Multi-agent specialization (optional)

- Add specialized agents:
  - monitoring triage
  - Kafka/event pipeline triage
  - strategy/execution diagnostics
- [x] Add coordinator agent (`services/agent-coordinator`) as supervisor/dispatcher (scaffold)
- Standardize inter-agent contracts

Exit criteria:

- Better MTTR than single agent
- No increase in false-positive action recommendations
- Coordinator traces available in LangSmith for all multi-agent incidents

Status: **In Progress (2026-05-07)**  
Notes:
- Added coordinator service/module:
  - `services/agent-coordinator/cmd/main.go`
  - `services/agent-coordinator/internal/config/config.go`
  - `services/agent-coordinator/internal/coordinator/coordinator.go`
  - `services/agent-coordinator/internal/coordinator/clients.go`
  - `services/agent-coordinator/internal/coordinator/types.go`
  - `services/agent-coordinator/internal/server/http_server.go`
- Added shared trace abstraction:
  - `shared/pkg/agent/trace.go`
- Added tests:
  - `services/agent-coordinator/internal/config/config_test.go`
  - `services/agent-coordinator/internal/coordinator/coordinator_test.go`
  - `shared/pkg/agent/trace_test.go`

## 9) Security and Compliance Requirements

- Secrets from external secret manager only (no prompt-embedded secrets)
- Redact credentials/account identifiers before model calls
- Network egress restrictions per agent service
- RBAC least privilege for all cluster operations
- Signed and immutable audit logs where feasible

## 10) Observability and SLOs for Agents

Track:

- Success/failure rate per workflow
- Mean triage latency
- Token and cost budget per run/day
- Tool call success/failure by tool type
- Recommendation acceptance ratio by operators

Target SLOs (initial):

- 99% successful run completion (read-only workflows)
- P95 triage report generation < 60s
- 100% of runs with auditable trace record

## 11) Testing Strategy

- Unit tests:
  - provider adapters
  - policy engine
  - tool adapters

- Integration tests:
  - alert ingestion to report generation
  - fallback behavior on provider/tool outages

- Offline eval suite:
  - replay historical incidents
  - compare hypothesis quality and recommendation relevance

- Chaos tests:
  - provider timeout
  - Prometheus unavailable
  - partial Kubernetes API failures

## 12) Deployment and Operations

- Deploy `ops-agent` to staging first via existing k8s overlays
- Enable canary strategy in production
- Start with business-hours activation window
- Provide immediate kill switch and fallback to deterministic runbooks

## 13) Suggested Repository Additions

- `services/ops-agent/` (new microservice)
- `shared/pkg/agent/` (interfaces, policies, orchestration contracts)
- `shared/pkg/agent/providers/{anthropic,openai}/`
- `shared/pkg/agent/tools/`
- `docs/agentic-ai/` (architecture, runbooks, evaluations)

## 14) Immediate Next Steps (Execution Order)

1. Create interface contracts in `shared/pkg/agent`.
2. Scaffold `services/ops-agent` with health/metrics endpoints.
3. Add Anthropic provider adapter first, OpenAI adapter second.
4. Integrate LangChain orchestration runtime behind adapter boundary.
5. Implement read-only monitoring tools and alert intake flow.
6. Build offline evaluation suite from recent incidents.
7. Run staging soak test and tune policies.

---

This plan intentionally prioritizes controlled, auditable operational intelligence first, then expands into deeper autonomy as confidence, tests, and governance mature.
