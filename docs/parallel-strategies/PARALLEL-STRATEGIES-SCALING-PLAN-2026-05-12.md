# Parallel Strategies — Scaling & Production Readiness Plan

Date: 2026-05-12  
Repository: `microservices-trading-bot`

## 1) Objective

Define a **production-ready, optimal approach** for running **multiple trading strategies in parallel** on the Bitso trading platform (Stage and Production), including:

- Correct use of **Kubernetes scaling** (replicas, HPA, node pools) versus **vertical sizing** (CPU/memory per pod and EC2 instance types in Terraform/EKS).
- Explicit handling of **known architectural constraints** in this repository (strategy-executor lifecycle, Kafka consumer groups, order-management single-replica assumptions, venue limits).
- A **phased roadmap** from safe experimentation to horizontally scalable execution, with exit criteria and observability gates.

This document complements:

- `docs/ORGANIC-TRADING-STARTUP.md` — organic (non-synthetic) strategy operation and cluster checks.
- `docs/LIMIT-PROFIT-STRATEGY.md`, `docs/LIMIT-PROFIT-ROBUSTNESS.md`, `docs/LIMIT-PROFIT-IMPROVEMENTS.md` — `limit_profit` behavior, lifecycle, and metrics.
- `docs/FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md` — broader strategy lifecycle and validation gates.
- `scripts/start-organic-trading.sh` — warns when `strategy-executor` replicas ≠ 1 for organic trading.

---

## 2) Definitions

| Term | Meaning in this document |
|------|---------------------------|
| **Parallel strategies** | More than one registered strategy instance (e.g. `limit_profit` + `momentum`, or two `limit_profit` configs on different books) evaluated on each tick cycle. |
| **Horizontal scale** | Increase **replica count** or **node count** so aggregate throughput rises. |
| **Vertical scale** | Increase **CPU/memory limits** per pod and/or **larger EC2 instance types** for worker nodes. |
| **Shard** | A dedicated deployment (or consumer group partition assignment) that owns a **subset** of books or strategy names so state and I/O do not conflict across replicas. |

---

## 3) Guiding Principles

1. **Measure before resizing** — Identify the bottleneck (CPU on executor, Kafka lag, Redis latency, Bitso HTTP 429/5xx, OM sync backlog) before changing instance families or replica counts.
2. **Do not conflate “bigger EC2” with “more strategies”** — Larger nodes help **CPU/memory-bound** work; they do **not** fix incorrect Kafka consumer topology, duplicate order submission, or missing shard routing.
3. **Respect single-writer and consumer-group semantics** — Some components are designed for **one active consumer per topic/group**; scaling replicas without redesign **duplicates work** or **splits state**.
4. **Venue is the ultimate throttle** — Bitso **rate limits**, **minimum order sizes**, and **maximum open orders** cap parallelism regardless of cluster size.
5. **Production safety** — Prefer **explicit sharding** or **one fat executor replica** over ambiguous multi-replica behavior until the codebase supports partition-aware strategy state.

---

## 4) Current Architecture Constraints (Repository-Specific)

### 4.1 Strategy-executor: in-process registry and organic loop

- Strategies are **registered in memory** in the strategy-executor process (`internal/strategies` registry pattern; see `docs/ORGANIC-TRADING-STARTUP.md` and `scripts/start-organic-trading.sh`).
- **Organic** execution assumes the poll loop and HTTP **create/start** target the **same** pod that receives ticks and publishes to Kafka.
- With **`replicas > 1`**:
  - API create/start may hit **pod A** while market-data ticks and indicator updates are processed on **pod B** → strategies **do not exist** on the pod handling ticks.
  - Order-fill consumption (`trading.order.fills`) may land on a pod **without** the matching strategy state → **stuck `pending_buy`**, missed exits, or inconsistent P&amp;L metadata.

**Implication:** For the **current** codebase, **multiple parallel strategies on one replica** is the **supported** test path. **Multiple replicas of the same strategy-executor Deployment** without sharding is **not production-ready** for organic trading.

### 4.2 Order-management: single replica by design (Kafka)

`k8s/base/order-management.yaml` documents that **replicas: 1** avoids competing consumers on `trading.orders.placed` and empty partition assignments.

**Implication:** **Do not scale order-management horizontally** until:

- A dedicated **orders-placed worker** exists with deterministic partition assignment, **or**
- Consumer logic is **idempotent** and safe under competing readers (not the current documented posture).

### 4.3 Trading-engine: horizontal scale with caveats

- Consumes **`trading.signals`**; multiple replicas can work **if** Kafka partition assignment gives each replica work without duplicate processing of the same message (standard consumer group semantics: **one consumer per partition per group**).
- Metrics and logs are **per-pod**; debugging “did my signal run?” may require identifying the consuming pod (see `docs/ORGANIC-TRADING-STARTUP.md` multi-replica note).

**Implication:** Scaling **trading-engine** can help **signal throughput** when the bottleneck is engine-side validation/HTTP to Bitso/OM — not when the bottleneck is **Bitso** or **OM single consumer**.

### 4.4 Redis and durable strategy state

- Indicators and optional `limit_profit` durable state depend on Redis (`docs/LIMIT-PROFIT-STRATEGY.md`).
- More strategies ⇒ more keys, more reads/writes per tick, higher **memory** and **connection** usage.

**Implication:** Monitor Redis **memory usage**, **eviction**, **slowlog**, and **connected_clients** as strategy count grows.

### 4.5 Kafka

- More strategies emit more messages on **`trading.signals`**, **`trading.orders.placed`**, **`trading.order.fills`** (names from config/trading-config).
- Risk areas: **producer batching**, **broker disk**, **consumer lag** per group, **partition count** vs desired parallelism.

### 4.6 Bitso (venue)

- Parallel strategies multiply **order placement attempts** and **open orders**.
- Hard limits: **rate limits**, **minimum notional/size**, **max open orders** per account/book.

**Implication:** Introduce **global throttles** and **per-strategy caps** in production; treat venue errors as first-class SLO drivers.

---

## 5) Recommended Production Approaches (Ordered by Maturity)

### 5.1 Tier A — Multiple strategies, single strategy-executor replica (short term, production-acceptable)

**What:** Run **N strategies** in **one** `strategy-executor` pod (`replicas: 1`), increase **pod resources** (and node capacity) as needed.

**Pros:**

- Matches **current** organic + API lifecycle assumptions.
- Simplest operations: one log stream, one metrics target, one place to inspect registry.

**Cons:**

- **Vertical ceiling**: one pod eventually CPU-throttles on heavy indicator math or large N.
- **Blast radius**: misbehaving strategy affects siblings (CPU, panics, noisy logging).

**When to use:** Stage soak tests, first production multi-strategy rollout with **small N** (e.g. 2–5).

**Infra actions:**

- Set meaningful **`resources.requests` / `limits`** on `strategy-executor`.
- Use **Cluster Autoscaler** / larger nodes only when profiling shows **scheduler cannot place** the pod or **CPU throttling** is sustained.

### 5.2 Tier B — Sharded strategy-executor (medium term, production-optimal for “many strategies”)

**What:** Run **multiple Deployments** (or Helm sub-charts) of strategy-executor, each with **`replicas: 1`**, each owning a **disjoint shard key**:

- **Shard by `book`** (e.g. `btc_mxn` vs `eth_mxn`), **or**
- **Shard by strategy family / tenant** (e.g. `team-a-*` vs `team-b-*`), **or**
- **Shard by risk tier** (conservative vs aggressive pools).

**Routing:**

- Market-data / tick ingress must deliver ticks **only** to the shard that owns that book (HTTP poll already scoped by config `INDICATORS_BOOKS`; ensure **no two shards poll the same book** unless intentionally duplicated for HA with different design).
- **Kafka consumers** on fills: each shard has its **own consumer group id suffix** or dedicated group so partitions are not fought over incorrectly (verify current `KAFKA_*` env patterns before cloning deployments).

**Pros:**

- True **horizontal** scale-out of **aggregate** strategy capacity while keeping **per-shard** replica count at 1.
- **Blast radius** isolation between shards.

**Cons:**

- More moving parts: **routing**, **config duplication**, **Grafana dashboards per shard**, **runbooks**.

**When to use:** Production when **N** is large or teams need isolation.

### 5.3 Tier C — Stateless executor workers + centralized state (long term, maximum scale)

**What:** Refactor so tick workers are **stateless** (or minimal cache), and **all strategy state** lives in **Redis** (or another store) with **atomic compare-and-swap** / leases; Kafka partition key = `book` or `strategy_id`; **exactly-once** or **idempotent** signal publishing.

**Pros:**

- **Horizontal** replicas safe; Kubernetes HPA becomes meaningful.
- Better resilience to pod restarts (state not tied to process).

**Cons:**

- **Largest engineering cost**; must revisit **ordering**, **race conditions**, **P&amp;L continuity**, and **fill correlation** (`event_id` / `signal_id`).

**When to use:** When Tier A/B cannot meet SLOs or cost targets.

---

## 6) EKS / Terraform: What to Change (and in What Order)

### 6.1 Node instance types (`module.eks` / node groups)

- **Use when:** kubelet shows **CPU throttling**, **OOMKills**, or **pending pods** due to insufficient allocatable resources on nodes **after** raising pod limits.
- **Avoid as first step:** switching to `c6i.8xlarge` without data — cost increases without fixing Kafka or Bitso bottlenecks.

**Practice:**

- Prefer **mixed instance types** + **multiple node groups** (e.g. general-purpose vs compute-optimized) over a single oversized pool.
- Align **pod requests** with node allocatable to reduce **fragmentation**.

### 6.2 Cluster scale (node count)

- **Use when:** **pending pods**, **high scheduling latency**, or **DaemonSet overhead** consumes too much per node.
- **Pair with:** Cluster Autoscaler or Karpenter policies, **max node** caps for cost control.

### 6.3 Pod replicas (HPA)

- **strategy-executor:** HPA on replicas is **not recommended** until Tier C or a **shard-aware** design exists.
- **trading-engine:** HPA **can** be valid if signals topic has **enough partitions** and consumer lag drives scaling; validate **no duplicate order submission** under retries and idempotency.
- **order-management:** **Do not HPA** without redesign (Section 4.2).

### 6.4 Kafka (MSK or in-cluster)

- If consumer lag grows with parallel strategies, evaluate:

  - **Partition count** for `trading.signals` vs max desired engine parallelism.
  - **Retention** and **disk** headroom.
  - **Producer acks** and **retry storms** when Bitso slows down.

### 6.5 Redis

- Move to **dedicated** Redis for trading (if shared), enable **maxmemory-policy** appropriate for workload, monitor **latency**.

---

## 7) Observability & SLOs (Parallel Strategy Run)

### 7.1 Must-watch metrics

| Area | Examples / intent |
|------|-------------------|
| **strategy-executor** | Tick loop duration, CPU throttling, `limit_profit_*` Prometheus metrics (`docs/LIMIT-PROFIT-STRATEGY.md`), publish failures. |
| **trading-engine** | Signals processed vs failed, order placement latency, Bitso error rate. |
| **order-management** | User-trades poller backlog, sync errors, **429/5xx** from Bitso, duplicate validation. |
| **Kafka** | Consumer **lag** per group/topic, broker disk, under-replicated partitions. |
| **Redis** | Memory, evictions, connections, command latency. |

### 7.2 SLO examples (initial)

- **P95 tick-to-publish** &lt; X ms per book (define X from Stage baseline).
- **Bitso rate limit errors** &lt; 1% of order attempts during soak.
- **Zero** sustained consumer lag &gt; N minutes on `trading.signals` / `trading.order.fills`.

---

## 8) Risk Controls for Parallel Strategies

1. **Global max concurrent orders** across strategies (OM or engine policy).
2. **Per-book notional caps** and **max daily loss** (`limit_profit` already supports `max_daily_loss_quote`; extend concept platform-wide if needed).
3. **Circuit breakers** on repeated venue errors (backoff + pause strategies via API).
4. **Kill switch** — stop all strategies (`StopAll` registry API where exposed) and optionally scale engine to 0 in emergency.

---

## 9) Testing Strategy

### 9.1 Unit / component

- Strategy math isolated per type (`go test ./internal/strategies/...`).
- Registry lifecycle: create/start/stop/remove does not leak goroutines.

### 9.2 Integration (Stage)

- **Two strategies, same book, different parameters** — verify **no double-fill** on same `event_id`, correct **signal_id** linkage in OM.
- **Two strategies, different books** — verify indicator isolation and Redis key separation.

### 9.3 Load / soak

- Ramp **N** over days; chart CPU, lag, Bitso errors, P&amp;L drift vs single-strategy baseline.

### 9.4 Chaos

- Kill strategy-executor pod mid-trade (with Redis on/off) to validate recovery expectations documented in `LIMIT-PROFIT` docs.

---

## 10) Phased Delivery Roadmap

Each phase lists **scope**, **deliverables**, **exit criteria**, and **infra posture** (what to scale and what not to).

---

## Phase 0 — Baseline & Guardrails (1–2 weeks)

**Scope**

- Document current **replica counts**, **Kafka topics/partitions**, **resource requests/limits**, and **Bitso** limits for Stage.
- Add dashboards/alerts for **consumer lag**, **executor CPU**, **Redis memory**, **venue errors**.

**Deliverables**

- Runbook section: “Adding a second strategy on Stage” (checklist: single executor replica, books in `trading-config`, Redis connected).
- **No** change to multi-replica strategy-executor for organic flows.

**Exit criteria**

- [ ] Single-strategy and two-strategy Stage runs complete with **SLO dashboard** populated.
- [ ] On-call knows **where to look** when lag or Bitso errors spike.

**Infra posture**

- **Keep** `strategy-executor` **replicas: 1** for organic.
- **Optional:** increase **pod limits** only if profiling shows CPU throttling at current strategy count.

**Status:** Planned  

**Notes:** Align with `docs/ORGANIC-TRADING-STARTUP.md` pod checks.

---

## Phase 1 — Safe Multi-Strategy on Single Executor (2–4 weeks)

**Scope**

- Run **2–N strategies** concurrently on **one** executor replica (different names/types/books as supported).
- Introduce **configuration caps**: max strategies, max signals/minute, per-book throttle (application config + validation at create time).

**Deliverables**

- Config + validation: reject create when limits exceeded.
- Metrics: **per-strategy** counters/histograms for tick duration and publish counts (extend existing `limit_profit_*` pattern to registry-level).

**Exit criteria**

- [ ] Stage soak **7 days** with **≥2 strategies** without manual intervention.
- [ ] **No** increase in Bitso error rate beyond agreed threshold vs single-strategy baseline.
- [ ] **P95** tick loop latency within target (define numerically from Phase 0 baseline).

**Infra posture**

- **Vertical:** raise `strategy-executor` **CPU/memory** requests/limits first.
- **Nodes:** increase instance size **only if** pods cannot get CPU/memory after limits raised.
- **Do not** scale `order-management` replicas.

**Status:** Planned  

**Notes:** This is the **recommended default production path** until shard or refactor work completes.

---

## Phase 2 — Sharded Strategy-Executor Deployments (4–8 weeks)

**Scope**

- Split strategies across **M deployments**, each `replicas: 1`, disjoint **shard keys** (by book or tenant).
- Automate via Kustomize overlays or Helm values: `SHARD_ID`, `INDICATORS_BOOKS`, Kafka consumer group suffixes.

**Deliverables**

- K8s manifests + docs for **shard operator** procedure (add shard, drain shard).
- Grafana: aggregated “all shards” dashboard via Prometheus federation or multi-cluster selectors.

**Exit criteria**

- [ ] **No book** processed by two shards simultaneously (static analysis in CI + runtime config test).
- [ ] Failover drill: kill one shard pod → **only** that shard’s strategies unavailable; others unaffected.

**Infra posture**

- **Horizontal:** more **small** executor pods (shards), not higher replicas per shard.
- **Cluster autoscaler:** scale nodes for **additional shard pods**.

**Status:** Planned  

**Notes:** This is the **production-optimal** approach for **high** parallel strategy count **without** Tier C refactor.

---

## Phase 3 — Trading-Engine Throughput Hardening (2–6 weeks, parallelizable)

**Scope**

- Validate **Kafka partition count** vs `trading-engine` max replicas.
- HPA based on **consumer lag** or **CPU** with safe max replicas.
- Idempotency review: signal replay does not double-place orders.

**Deliverables**

- HPA manifests + load test report.
- Document “max safe replicas” per partition layout.

**Exit criteria**

- [ ] Engine replicas &gt;1 pass integration tests with **no duplicate Bitso orders** for same `event_id`.
- [ ] Lag-driven HPA stable (no flapping) under 30-min soak.

**Infra posture**

- Scale **engine** before adding **shard** executors if the bottleneck is **engine** not **strategy evaluation**.

**Status:** Planned  

**Notes:** Coordinate with Phase 2 to avoid over-scaling into Bitso limits.

---

## Phase 4 — Order-Management Scaling Design (6–12 weeks)

**Scope**

- Extract **Kafka consumers** into dedicated worker Deployment(s) with explicit partition strategy **or** redesign OM for **safe** multi-replica consumption.

**Deliverables**

- ADR (Architecture Decision Record): chosen consumer model.
- Implementation + migration plan from `replicas: 1`.

**Exit criteria**

- [ ] OM (or workers) **≥2 replicas** with **no duplicate order side-effects** in stress tests.
- [ ] Consumer lag SLO met under 2× current signal volume.

**Infra posture**

- **Horizontal** workers; OM HTTP API may remain fewer replicas behind Service.

**Status:** Planned  

**Notes:** **Highest risk** phase; do not start until Phases 1–3 are stable.

---

## Phase 5 — Tier C Refactor (Stateless Executor) (quarter+)

**Scope**

- Centralize strategy state; partition-aware tick processing; HA executor replicas.

**Deliverables**

- Design doc + phased migration from Tier B shards to unified pool.

**Exit criteria**

- [ ] `strategy-executor` **HPA** validated under chaos (pod kill) without stuck orders.
- [ ] P&amp;L and fill attribution audited against legacy behavior.

**Status:** Future  

**Notes:** Only when business need justifies engineering investment.

---

## 11) Decision Matrix (Quick Reference)

| Symptom | Likely first lever | Avoid |
|---------|-------------------|--------|
| High **executor** CPU, single replica | Raise **pod CPU** / optimize hot paths | Adding 2nd executor replica (organic) |
| **Pending pods**, low allocatable | **More/larger nodes**, tune requests | Blind replica increase |
| **Kafka lag** (signals) | More **partitions** / more **engine** replicas (if safe) | Bigger EC2 only |
| **Redis** OOM / slow | Memory class, **cluster mode**, key TTL review | More strategies without Redis headroom |
| **Bitso 429** | **Throttle** strategies, fewer concurrent orders | More engine replicas |

---

## 12) Suggested Repository / Ops Additions

- `docs/parallel-strategies/` (this folder) — scaling ADRs, shard diagrams, load-test results.
- `docs/runbooks/PARALLEL-STRATEGIES-RUNBOOK.md` (future) — incident steps for lag, duplicate orders, shard failover.
- Kustomize overlay examples: `strategy-executor-shard-a`, `strategy-executor-shard-b` (future).

---

## 13) Summary

- **Short-term production approach:** **many strategies, one `strategy-executor` replica**, scale **vertically** (pod then node) and enforce **venue-aware throttles**.
- **Medium-term optimal approach:** **horizontally scale out via sharded executor deployments**, each still **`replicas: 1`**, with **disjoint books/tenants** and correct Kafka consumer identity.
- **Long-term maximum scale:** **stateless workers + centralized state** (Tier C).
- **Do not** treat “larger EC2 in Terraform” or “more EKS nodes” as the primary strategy for parallel strategies without **evidence**; pair infrastructure changes with **consumer topology** and **Bitso** constraints for a production-ready system.

---

This plan intentionally prioritizes **correctness and observability** (single-replica executor, shard path, venue limits) before aggressive horizontal scaling of components that are not yet safe to duplicate.
