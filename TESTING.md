# Testing

## Run all tests

```bash
go mod tidy
go test ./... -count=1
```

## Coverage

```bash
go test ./... -coverprofile=coverage.out -count=1
go tool cover -func=coverage.out
```

Or use `./scripts/run-tests.sh`.

## Conventions

- **testify** (`assert` / `require`) for expectations
- **httptest** + Gin test mode for handlers and middleware
- **miniredis** for Redis (no real Redis)
- **kubernetes/fake** and **dynamic/fake** for operator provisioning tests
- **In-memory mocks** in `internal/mocks` for `UserStore`, `ProjectStore`, and `InstanceProvisioner`

No tests connect to real PostgreSQL, MongoDB, Redis, Kubernetes, or Google APIs.
