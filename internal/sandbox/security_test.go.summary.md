# security_test.go

## Purpose
Security-focused tests for the sandbox package that verify isolation boundaries and safety constraints. These tests specifically target edge cases and potential attack vectors: malformed sandbox IDs that could cause path traversal, overly permissive network policies that should be rejected, resource limit enforcement, and vsock CID boundary values.

## Key Types and Functions
- `TestSandboxIDPathTraversal`: verifies that sandbox IDs containing `../` or other path traversal sequences are rejected by `Create`
- `TestNetworkPolicy_DefaultDenyRequired`: verifies that a `NetworkPolicy` with `DefaultDeny=false` is rejected
- `TestNetworkPolicy_WildcardHostRejected`: verifies that wildcard host entries (`*`, `*.example.com`) are rejected in the allow list
- `TestNetworkPolicy_CIDRAllZerosRejected`: verifies that `0.0.0.0/0` and `::/0` are rejected in the allow list
- `TestVCPULimits`: verifies that VCPUs below 1 or above 32 are rejected
- `TestMemoryLimits`: verifies that MemoryMB below 128 or above 32768 is rejected
- `TestVsockCIDMinimum`: verifies that CID values below 3 are rejected (0=host, 1=hypervisor, 2=reserved)

## Role in the System
Acts as a security regression test suite for the sandbox spec validation layer. These tests directly guard against misconfigured sandboxes that could break isolation or allow resource abuse.

## Dependencies
- `testing`
- `internal/sandbox`: spec types and `FirecrackerRuntime`
