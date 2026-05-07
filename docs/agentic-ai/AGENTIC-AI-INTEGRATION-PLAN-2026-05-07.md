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

## 8) Phased Delivery Roadmap

## Phase 0 - Foundations (1 week)

- Define interfaces in `shared/pkg/agent`
- Implement provider adapters skeleton (`anthropic`, `openai`)
- Add policy engine, budget model, and audit schema
- Add feature flags and kill-switch

Exit criteria:

- Unit tests pass for provider contracts and policy checks
- Can switch provider by config only

## Phase 1 - Read-only Ops Agent (2 weeks)

- Build `services/ops-agent`
- Integrate Alertmanager webhook intake
- Integrate read-only tool adapters
- Generate incident report payloads
- Add metrics (`agent_runs_total`, `agent_run_latency_ms`, `agent_failures_total`)

Exit criteria:

- Produces useful triage reports for known failure scenarios
- No write operations enabled
- Audit logs complete

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
- Add supervisor/dispatcher
- Standardize inter-agent contracts

Exit criteria:

- Better MTTR than single agent
- No increase in false-positive action recommendations

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
