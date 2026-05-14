package runtime

import (
	"context"
	"net/http/httptest"

	"github.com/PixnBits/AegisClaw/internal/testutil"
)

// Minimal stubs for SDLC tests

type SDLCStatus struct {
	Phase         string `json:"phase"`
	CourtApproved bool   `json:"courtApproved"`
	CodeGenerated bool   `json:"codeGenerated"`
	PRURL         string `json:"prURL"`
	Deployed      bool   `json:"deployed"`
	Error         string `json:"error"`
}

func StartAegisClawWithPortal(ctx context.Context) *testutil.TestServer {
	srv := httptest.NewServer(nil) // Real-like stub; expand as needed
	return &testutil.TestServer{URL: srv.URL}
}

func RunBuilderPipeline(ctx context.Context, p interface{}) (BuildResult, error) {
	return BuildResult{Success: true, ProposalBranch: "feat/test-skill"}, nil
}

type BuildResult struct {
	Success       bool
	ProposalBranch string
}

func NewDeployer(ctx context.Context) *Deployer {
	return &Deployer{}
}

type Deployer struct{}

func (d *Deployer) Deploy(ctx context.Context, skill string) (string, error) {
	return "sandbox-123", nil
}

func NewSecurityGates(ctx context.Context) *SecurityGates {
	return &SecurityGates{}
}

type SecurityGates struct{}

func (g *SecurityGates) RunAll(ctx context.Context) (GateResults, error) {
	return GateResults{AllPassed: true}, nil
}

type GateResults struct {
	AllPassed bool
}

func NewDeploymentPreview(ctx context.Context) *DeploymentPreview {
	return &DeploymentPreview{}
}

type DeploymentPreview struct{}

func (p *DeploymentPreview) Generate(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func InvokeSkill(ctx context.Context, sandboxID, method string, payload interface{}) (InvocationResult, error) {
	return InvocationResult{Output: "vision analysis complete"}, nil
}

type InvocationResult struct {
	Output string
}

func CheckHealth(ctx context.Context, sandboxID string) Health {
	return Health{Healthy: true}
}

type Health struct {
	Healthy bool
}
