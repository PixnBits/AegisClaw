# Phase 5: Concrete Tests Added

## New Tests in `cmd/aegisclaw/daemon_test.go`

- `TestCreateSecureSocket_SetsStrictPermissions` — verifies socket gets `0600`
- `TestCreateSecureSocket_CreatesParentDirWith0700` — verifies directory permissions
- `TestLifecycleContainment_RegistersSignalHandlers`
- `TestDropCapabilities_DoesNotPanic`
- `TestApplySeccompFilter_DoesNotPanic`
- `TestNoBusinessLogicInDaemon` (policy guard)
- `TestNoSecretHandlingInDaemon` (policy guard)

These provide concrete, runnable verification for the most critical hardening properties.