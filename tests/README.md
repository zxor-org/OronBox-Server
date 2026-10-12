# Tests

Tests that need to exercise the server through an exported package interface
or a real external dependency live here. Package unit tests remain next to the
implementation under `internal/` so they can cover package-private invariants.

## Integration tests

`tests/integration` contains tests that use external services such as
PostgreSQL. They are included by `go test ./...` and skip when their required
environment variables are absent. Set `TEST_DATABASE_URL` to run the database
integration tests locally.
