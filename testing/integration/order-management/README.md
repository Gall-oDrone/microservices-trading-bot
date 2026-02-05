# Order-Management Integration Tests

Order-management **component integration tests** live in the service module so they can use `internal` packages:

**Location:** `services/order-management/integration/`

See that directory’s [README](../../../services/order-management/integration/README.md) for what is tested and how to run:

```bash
cd services/order-management && go test ./integration/... -v
```
