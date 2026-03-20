package llm

import (
	"fmt"
	"net"
	"net/url"

	"go.uber.org/zap"
)

// OllamaPort is the standard Ollama API port.
const OllamaPort uint16 = 11434

// OllamaHost is the only allowed host for Ollama connections.
const OllamaHost = "127.0.0.1"

// IsolationPolicy defines where Ollama calls are permitted.
type IsolationPolicy struct {
	// RequireSandbox, when true, rejects calls from non-sandboxed contexts.
	RequireSandbox bool
	// AllowedHosts restricts which hosts may be used for the Ollama endpoint.
	AllowedHosts []string
	// AllowedPorts restricts which ports may be used for the Ollama endpoint.
	AllowedPorts []uint16
}

// DefaultIsolationPolicy returns the recommended production isolation policy:
// sandbox-only, localhost, port 11434.
func DefaultIsolationPolicy() IsolationPolicy {
	return IsolationPolicy{
		RequireSandbox: true,
		AllowedHosts:   []string{OllamaHost},
		AllowedPorts:   []uint16{OllamaPort},
	}
}

// IsolationContext describes the caller's execution context for policy checks.
type IsolationContext struct {
	// InSandbox indicates whether the caller is inside a Firecracker sandbox.
	InSandbox bool
	// SandboxID is the ID of the sandbox (empty if not sandboxed).
	SandboxID string
	// CallerType describes who is making the call (e.g., "reviewer", "builder", "kernel").
	CallerType string
}

// IsolationError is returned when an Ollama call violates isolation policy.
type IsolationError struct {
	Reason     string
	CallerType string
	Endpoint   string
}

func (e *IsolationError) Error() string {
	return fmt.Sprintf("ollama isolation violation: %s (caller=%s, endpoint=%s)", e.Reason, e.CallerType, e.Endpoint)
}

// IsolationEnforcer validates that Ollama calls comply with the isolation policy.
type IsolationEnforcer struct {
	policy IsolationPolicy
	logger *zap.Logger
}

// NewIsolationEnforcer creates an enforcer with the given policy.
func NewIsolationEnforcer(policy IsolationPolicy, logger *zap.Logger) *IsolationEnforcer {
	return &IsolationEnforcer{
		policy: policy,
		logger: logger,
	}
}

// Check validates that the given context and endpoint comply with the isolation policy.
// Returns nil if the call is permitted, or an IsolationError if rejected.
func (ie *IsolationEnforcer) Check(ctx IsolationContext, endpoint string) error {
	// Kernel is never allowed to call Ollama directly
	if ctx.CallerType == "kernel" {
		ie.logger.Error("blocked Ollama call from kernel",
			zap.String("endpoint", endpoint),
		)
		return &IsolationError{
			Reason:     "kernel process must not call Ollama directly",
			CallerType: ctx.CallerType,
			Endpoint:   endpoint,
		}
	}

	// Enforce sandbox requirement
	if ie.policy.RequireSandbox && !ctx.InSandbox {
		ie.logger.Error("blocked Ollama call from non-sandboxed context",
			zap.String("caller_type", ctx.CallerType),
			zap.String("endpoint", endpoint),
		)
		return &IsolationError{
			Reason:     "Ollama calls require sandbox execution context",
			CallerType: ctx.CallerType,
			Endpoint:   endpoint,
		}
	}

	// Validate endpoint against allowed hosts and ports
	if err := ie.checkEndpoint(ctx, endpoint); err != nil {
		return err
	}

	return nil
}

func (ie *IsolationEnforcer) checkEndpoint(ctx IsolationContext, endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return &IsolationError{
			Reason:     fmt.Sprintf("invalid endpoint URL: %v", err),
			CallerType: ctx.CallerType,
			Endpoint:   endpoint,
		}
	}

	host := parsed.Hostname()
	port := parsed.Port()

	// Check allowed hosts
	if len(ie.policy.AllowedHosts) > 0 {
		allowed := false
		for _, h := range ie.policy.AllowedHosts {
			if h == host {
				allowed = true
				break
			}
		}
		if !allowed {
			ie.logger.Error("blocked Ollama call to disallowed host",
				zap.String("host", host),
				zap.Strings("allowed_hosts", ie.policy.AllowedHosts),
			)
			return &IsolationError{
				Reason:     fmt.Sprintf("host %q not in allowed hosts", host),
				CallerType: ctx.CallerType,
				Endpoint:   endpoint,
			}
		}
	}

	// Check allowed ports
	if len(ie.policy.AllowedPorts) > 0 && port != "" {
		portNum, err := net.LookupPort("tcp", port)
		if err != nil {
			return &IsolationError{
				Reason:     fmt.Sprintf("invalid port: %v", err),
				CallerType: ctx.CallerType,
				Endpoint:   endpoint,
			}
		}
		allowed := false
		for _, p := range ie.policy.AllowedPorts {
			if uint16(portNum) == p {
				allowed = true
				break
			}
		}
		if !allowed {
			ie.logger.Error("blocked Ollama call to disallowed port",
				zap.String("port", port),
				zap.Any("allowed_ports", ie.policy.AllowedPorts),
			)
			return &IsolationError{
				Reason:     fmt.Sprintf("port %s not in allowed ports", port),
				CallerType: ctx.CallerType,
				Endpoint:   endpoint,
			}
		}
	}

	return nil
}

// ValidateNetworkPolicy checks that a sandbox's NetworkPolicy allows Ollama access
// on the correct host and port while maintaining default-deny.
func ValidateNetworkPolicy(defaultDeny bool, allowedHosts []string, allowedPorts []uint16) error {
	if !defaultDeny {
		return fmt.Errorf("network policy must have default_deny=true")
	}

	hasOllamaHost := false
	for _, h := range allowedHosts {
		if h == OllamaHost {
			hasOllamaHost = true
			break
		}
	}
	if !hasOllamaHost {
		return fmt.Errorf("network policy must allow host %s for Ollama access", OllamaHost)
	}

	hasOllamaPort := false
	for _, p := range allowedPorts {
		if p == OllamaPort {
			hasOllamaPort = true
			break
		}
	}
	if !hasOllamaPort {
		return fmt.Errorf("network policy must allow port %d for Ollama access", OllamaPort)
	}

	return nil
}

// ForbiddenCallerTypes are context types that must never make direct Ollama calls.
var ForbiddenCallerTypes = []string{"kernel", "cli"}

// IsForbiddenCaller checks if the given caller type is in the forbidden list.
func IsForbiddenCaller(callerType string) bool {
	for _, f := range ForbiddenCallerTypes {
		if f == callerType {
			return true
		}
	}
	return false
}
