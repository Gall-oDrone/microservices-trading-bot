# Order-Management Internal Tests

This folder holds **black-box** tests that use only the **exported API** of their target packages (manager, metrics).

## Layout

| Location | What | Why |
|----------|------|-----|
| `internal/testing/` | Manager tests (`manager_test.go`) | Package `managertest`; manager API tests only |
| `internal/testing/metrics/` | Intraday aggregator tests (`intraday_aggregator_test.go`) | Package `metricstest`; tests `metrics.NewIntradayAggregator` and public methods via a mock writer |
| `internal/manager/` | `state_machine_test.go` only | Tests unexported `StateMachine`; must stay in package `manager` to access `NewStateMachine` and internal types |

**Tests that stay in their packages (not here):** `validator`, `risk`, `repository`, and `config` keep `*_test.go` next to code because they use package-internal setup (e.g. `setupValidator()`, `setupRiskManager()` with unexported fields, in-memory repos). Moving them would require exporting more constructors or duplicating setup; the current in-package tests are appropriate for those units.

## Running tests

```bash
# All black-box tests in internal/testing (manager + metrics)
go test ./internal/testing/...


# State machine (in-package) tests
go test ./internal/manager/...
```

## Is this the best approach?

**Trade-offs:**

- **Centralized `internal/testing/` (this approach)**  
  - **Pros:** Black-box tests for manager and metrics in one place; clear separation; tests use only public API, which can improve design and refactoring safety.  
  - **Cons:** Differs from the common Go style of `*_test.go` next to the code; validator, risk, repository, and config still use in-package tests.

- **Standard Go: tests next to code**  
  - **Pros:** Matches common Go practice; `go test ./...` discovers everything; tests can exercise unexported helpers when needed.  
  - **Cons:** Manager/metrics would again mix in-package tests and black-box tests in one directory.

**Recommendation:** For a production-ready codebase, both are valid. This repo uses a **hybrid**: manager and metrics **API** tests live under `internal/testing/`; **internal implementation** tests (e.g. state machine) and component unit tests (validator, risk, repository, config) stay next to their code.
