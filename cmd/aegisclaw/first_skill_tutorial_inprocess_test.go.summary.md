# first_skill_tutorial_inprocess_test.go — cmd/aegisclaw

## Purpose
Mirrors `first_skill_tutorial_test.go` but uses `InProcessSandboxLauncher` so no KVM, Firecracker, root, or rootfs images are required. Gated by the `inprocesstest` build tag and `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only`.

## Security Warning
`InProcessSandboxLauncher` has **zero sandbox isolation**. Must not run in production or standard CI.

## System Fit
Faster variant of the tutorial journey test for local development iteration.

## Notable Dependencies
- Build tag: `inprocesstest`
- `AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only` env var
