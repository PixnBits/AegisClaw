//go:build inprocesstest
// +build inprocesstest

// ╔══════════════════════════════════════════════════════════════════════════╗
// ║  WARNING — TEST-ONLY CODE — NOT FOR PRODUCTION USE                      ║
// ║                                                                          ║
// ║  This file is compiled ONLY when the "inprocesstest" build tag is set.  ║
// ║  It MUST NOT appear in any production binary, release build, or         ║
// ║  default "go test ./..." run.                                            ║
// ║                                                                          ║
// ║  InProcessTaskExecutor has ZERO isolation: it runs agent logic directly  ║
// ║  in the test process with no Firecracker microVM, no jailer, and no     ║
// ║  capability dropping. There is NO security boundary.                    ║
// ║                                                                          ║
// ║  See CONTRIBUTING.md for safe usage instructions.                       ║
// ╚══════════════════════════════════════════════════════════════════════════╝

package exec

import (
	"context"
	"fmt"
	"os"
)

// InProcessEnvVar is the environment variable that must be set to the exact
// value InProcessEnvValue before InProcessTaskExecutor will operate.
// This is a hard safety guard: any test that uses this executor MUST set the
// variable, confirming awareness that there is no sandbox isolation.
const (
	InProcessEnvVar   = "AEGISCLAW_INPROCESS_TEST_MODE"
	InProcessEnvValue = "unsafe_for_testing_only"
)

// AgentFunc is a function that implements one turn of the agent ReAct loop.
// Tests provide a stub that returns deterministic responses; a real
// implementation might call Ollama directly.
type AgentFunc func(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error)

// InProcessTaskExecutor implements TaskExecutor entirely inside the current
// process — no Firecracker, no jailer, no vsock.
//
// SECURITY: This executor provides ZERO isolation. It is exclusively for
// fast in-process integration testing. Production builds cannot contain this
// code because it is compiled only under the "inprocesstest" build tag.
//
// Safety guards (both must pass at construction time):
//  1. AEGISCLAW_INPROCESS_TEST_MODE must equal "unsafe_for_testing_only".
//  2. A loud, multi-line warning is printed to stderr on every activation.
type InProcessTaskExecutor struct {
	agentFn AgentFunc
}

// NewInProcessExecutor creates an InProcessTaskExecutor backed by agentFn.
//
// It panics immediately if AEGISCLAW_INPROCESS_TEST_MODE is not set to the
// required sentinel value.  This is intentional: the panic makes it
// impossible to accidentally use this executor in an environment where the
// variable is not set.
func NewInProcessExecutor(agentFn AgentFunc) *InProcessTaskExecutor {
	if os.Getenv(InProcessEnvVar) != InProcessEnvValue {
		panic(fmt.Sprintf(
			"NewInProcessExecutor: safety guard failed.\n"+
				"Set %s=%s to acknowledge that this executor has NO isolation.\n"+
				"This executor must NEVER be used outside of test code.",
			InProcessEnvVar, InProcessEnvValue,
		))
	}

	printInProcessWarning()

	return &InProcessTaskExecutor{agentFn: agentFn}
}

// ExecuteTurn implements TaskExecutor by calling the configured AgentFunc
// directly in the current process.
func (e *InProcessTaskExecutor) ExecuteTurn(ctx context.Context, req AgentTurnRequest) (AgentTurnResponse, error) {
	if e.agentFn == nil {
		return AgentTurnResponse{}, fmt.Errorf("inprocess executor: agentFn is nil")
	}
	return e.agentFn(ctx, req)
}

// printInProcessWarning writes a highly visible warning banner to stderr.
// Called once per NewInProcessExecutor invocation.
func printInProcessWarning() {
	const banner = `
╔══════════════════════════════════════════════════════════════════════════════╗
║  !!  AEGISCLAW IN-PROCESS TEST EXECUTOR ACTIVE  !!                         ║
║                                                                              ║
║  This executor runs agent logic DIRECTLY IN THE TEST PROCESS.               ║
║  There is NO Firecracker microVM, NO jailer, and NO capability dropping.    ║
║  There is ZERO security isolation.                                           ║
║                                                                              ║
║  This mode exists ONLY for fast integration testing during development.     ║
║  It MUST NEVER appear in a production binary or release build.              ║
║                                                                              ║
║  If you see this message outside of a test run, STOP IMMEDIATELY and        ║
║  report it as a security incident.                                           ║
╚══════════════════════════════════════════════════════════════════════════════╝
`
	fmt.Fprint(os.Stderr, banner)
}
