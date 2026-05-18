# Integration Lifecycle Tests Added

- Created `lifecycle_integration_test.go` with `//go:build integration`
- Tests for monitor health/restart threshold and clean shutdown
- Can be run locally with `-tags=integration`
- Designed to be opt-in in CI (richer environments)