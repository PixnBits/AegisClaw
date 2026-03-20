package llm

import (
	"testing"

	"go.uber.org/zap"
)

func TestDefaultIsolationPolicy(t *testing.T) {
	p := DefaultIsolationPolicy()
	if !p.RequireSandbox {
		t.Error("expected RequireSandbox=true")
	}
	if len(p.AllowedHosts) != 1 || p.AllowedHosts[0] != OllamaHost {
		t.Errorf("expected [%s], got %v", OllamaHost, p.AllowedHosts)
	}
	if len(p.AllowedPorts) != 1 || p.AllowedPorts[0] != OllamaPort {
		t.Errorf("expected [%d], got %v", OllamaPort, p.AllowedPorts)
	}
}

func TestIsolationCheckKernelBlocked(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		CallerType: "kernel",
	}, "http://127.0.0.1:11434")
	if err == nil {
		t.Error("expected error for kernel caller")
	}
	isoErr, ok := err.(*IsolationError)
	if !ok {
		t.Fatalf("expected IsolationError, got %T", err)
	}
	if isoErr.CallerType != "kernel" {
		t.Errorf("expected kernel caller, got %q", isoErr.CallerType)
	}
}

func TestIsolationCheckNonSandboxBlocked(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  false,
		CallerType: "reviewer",
	}, "http://127.0.0.1:11434")
	if err == nil {
		t.Error("expected error for non-sandboxed context")
	}
}

func TestIsolationCheckSandboxedReviewerAllowed(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		SandboxID:  "sandbox-123",
		CallerType: "reviewer",
	}, "http://127.0.0.1:11434")
	if err != nil {
		t.Errorf("expected allowed, got: %v", err)
	}
}

func TestIsolationCheckSandboxedBuilderAllowed(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		SandboxID:  "builder-456",
		CallerType: "builder",
	}, "http://127.0.0.1:11434")
	if err != nil {
		t.Errorf("expected allowed, got: %v", err)
	}
}

func TestIsolationCheckWrongHost(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		CallerType: "reviewer",
	}, "http://192.168.1.100:11434")
	if err == nil {
		t.Error("expected error for wrong host")
	}
}

func TestIsolationCheckWrongPort(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		CallerType: "reviewer",
	}, "http://127.0.0.1:8080")
	if err == nil {
		t.Error("expected error for wrong port")
	}
}

func TestIsolationCheckInvalidURL(t *testing.T) {
	ie := NewIsolationEnforcer(DefaultIsolationPolicy(), zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		CallerType: "reviewer",
	}, "://invalid")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestIsolationCheckNoSandboxRequirement(t *testing.T) {
	policy := IsolationPolicy{
		RequireSandbox: false,
		AllowedHosts:   []string{OllamaHost},
		AllowedPorts:   []uint16{OllamaPort},
	}
	ie := NewIsolationEnforcer(policy, zap.NewNop())
	err := ie.Check(IsolationContext{
		InSandbox:  false,
		CallerType: "reviewer",
	}, "http://127.0.0.1:11434")
	if err != nil {
		t.Errorf("expected allowed without sandbox requirement, got: %v", err)
	}
}

func TestIsolationCheckEmptyAllowedHosts(t *testing.T) {
	policy := IsolationPolicy{
		RequireSandbox: false,
		AllowedHosts:   nil,
		AllowedPorts:   []uint16{OllamaPort},
	}
	ie := NewIsolationEnforcer(policy, zap.NewNop())
	// With no host restrictions, any host should be allowed
	err := ie.Check(IsolationContext{
		InSandbox:  true,
		CallerType: "reviewer",
	}, "http://10.0.0.1:11434")
	if err != nil {
		t.Errorf("expected allowed with empty host list, got: %v", err)
	}
}

func TestValidateNetworkPolicy(t *testing.T) {
	// Valid policy
	err := ValidateNetworkPolicy(true, []string{"127.0.0.1"}, []uint16{11434})
	if err != nil {
		t.Errorf("expected valid, got: %v", err)
	}

	// Missing default deny
	err = ValidateNetworkPolicy(false, []string{"127.0.0.1"}, []uint16{11434})
	if err == nil {
		t.Error("expected error for missing default deny")
	}

	// Missing Ollama host
	err = ValidateNetworkPolicy(true, []string{"10.0.0.1"}, []uint16{11434})
	if err == nil {
		t.Error("expected error for missing Ollama host")
	}

	// Missing Ollama port
	err = ValidateNetworkPolicy(true, []string{"127.0.0.1"}, []uint16{8080})
	if err == nil {
		t.Error("expected error for missing Ollama port")
	}
}

func TestIsForbiddenCaller(t *testing.T) {
	if !IsForbiddenCaller("kernel") {
		t.Error("kernel should be forbidden")
	}
	if !IsForbiddenCaller("cli") {
		t.Error("cli should be forbidden")
	}
	if IsForbiddenCaller("reviewer") {
		t.Error("reviewer should not be forbidden")
	}
	if IsForbiddenCaller("builder") {
		t.Error("builder should not be forbidden")
	}
}

func TestIsolationErrorMessage(t *testing.T) {
	err := &IsolationError{
		Reason:     "test reason",
		CallerType: "kernel",
		Endpoint:   "http://127.0.0.1:11434",
	}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if msg != "ollama isolation violation: test reason (caller=kernel, endpoint=http://127.0.0.1:11434)" {
		t.Errorf("unexpected message: %s", msg)
	}
}
