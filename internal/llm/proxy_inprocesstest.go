//go:build inprocesstest
// +build inprocesstest

// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  WARNING — TEST-ONLY CODE — NOT FOR PRODUCTION USE                      ║
// ║                                                                          ║
// ║  This file is compiled ONLY when the "inprocesstest" build tag is set.  ║
// ║  It MUST NOT appear in any production binary, release build, or         ║
// ║  default "go test ./..." run.                                            ║
// ╚══════════════════════════════════════════════════════════════════════════╝

package llm

// InferDirect calls handleRequest directly, bypassing vsock transport.
//
// It exists solely so that InProcessSandboxLauncher (internal/court) can
// drive LLM inference in the test process without standing up a Firecracker VM.
// The audit write path inside handleRequest is preserved, so llm.infer entries
// are still recorded in the tamper-evident kernel log during inprocess tests.
//
// This method is compiled ONLY under the "inprocesstest" build tag.
// It MUST NOT appear in any production binary.
func (p *OllamaProxy) InferDirect(vmID string, req *ProxyRequest) ProxyResponse {
	return p.handleRequest(vmID, req)
}
