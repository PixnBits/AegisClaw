# isolation_test.go

## Purpose
Unit tests for the `IsolationEnforcer` and related helpers in `isolation.go`. Verifies that all policy rejection and acceptance paths behave correctly.

## Key Test Cases
- **`TestDefaultIsolationPolicy`** – checks `RequireSandbox=true`, single allowed host (`127.0.0.1`), and single allowed port (`11434`).
- **`TestIsolationCheckKernelBlocked`** – asserts `"kernel"` caller type is always rejected, even when `InSandbox=true`.
- **`TestIsolationCheckNonSandboxBlocked`** – asserts non-sandboxed reviewer is rejected under the default policy.
- **`TestIsolationCheckSandboxedReviewerAllowed`** / **`TestIsolationCheckSandboxedBuilderAllowed`** – happy paths for legitimate sandboxed callers.
- **`TestIsolationCheckWrongHost`** / **`TestIsolationCheckWrongPort`** – endpoint allowlist rejection cases.
- **`TestIsolationCheckInvalidURL`** – confirms an unparseable endpoint string is rejected.
- **`TestIsolationCheckNoSandboxRequirement`** – verifies that `RequireSandbox=false` allows non-sandboxed callers.
- **`TestIsolationCheckEmptyAllowedHosts`** – confirms that an empty host list means "any host allowed".
- **`TestValidateNetworkPolicy`** – tests valid policy, missing default-deny, missing Ollama host, and missing Ollama port.
- **`TestIsForbiddenCaller`** – verifies `"kernel"` and `"cli"` are forbidden; `"reviewer"` and `"builder"` are not.
- **`TestIsolationErrorMessage`** – checks the structured error string format.

## System Role
Regression suite for the access-control policy layer. All tests are self-contained with no external dependencies.

## Notable Dependencies
- `go.uber.org/zap` – `zap.NewNop()` for silent logging in tests.
