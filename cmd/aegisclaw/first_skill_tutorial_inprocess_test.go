//go:build inprocesstest
// +build inprocesstest

// TestFirstSkillTutorialInProcess — mirrors TestFirstSkillTutorialLive but uses
// InProcessSandboxLauncher so no KVM, Firecracker, root, or rootfs is required.
//
// SECURITY WARNING: This test uses InProcessSandboxLauncher which has ZERO
// sandbox isolation.  It MUST ONLY be run with the "inprocesstest" build tag
// AND the environment variable AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only.
//
// Normal "go test ./..." does NOT compile or run this test.
//
// To run:
//
//	AEGISCLAW_INPROCESS_TEST_MODE=unsafe_for_testing_only \
//	  go test ./cmd/aegisclaw -tags=inprocesstest -run TestFirstSkillTutorialInProcess -v
//
// Or via Makefile:
//
//	make test-inprocess
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/audit"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/testutil"
	"go.uber.org/zap/zaptest"
)

// TestFirstSkillTutorialInProcess exercises the same proposal-creation and
// court-review flow as TestFirstSkillTutorialLive, but replaces every
// Firecracker microVM with an InProcessSandboxLauncher:
//
//   - Reviewer LLM inference runs directly in the test process via OllamaProxy.InferDirect.
//   - Ollama HTTP responses are replayed from the existing cassette
//     (testdata/cassettes/first-skill-tutorial-live.yaml) — no live Ollama daemon needed.
//   - No KVM, no Firecracker binary, no rootfs template, and no root privileges required.
//
// The test verifies:
//  1. The court engine reaches a terminal state (approved / rejected / escalated).
//  2. The audit log contains at least one llm.infer entry, proving that inference
//     ran through the auditing path inside OllamaProxy even without vsock.
func TestFirstSkillTutorialInProcess(t *testing.T) {
	skipUnlessInProcessMode(t)

	if !testutil.OllamaCassetteExists("first-skill-tutorial-live") {
		t.Skip("replay mode requires testdata/cassettes/first-skill-tutorial-live.yaml; record it once with RECORD_OLLAMA=true sudo ./scripts/run-live-test.sh")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// ── Reset package-level singletons ───────────────────────────────
	kernel.ResetInstance()
	runtimeOnce = sync.Once{}
	runtimeInst = nil
	registryInst = nil
	proposalInst = nil
	compositionInst = nil
	runtimeInitErr = nil

	// ── Initialise kernel, proposal store, and OllamaProxy ───────────
	logger := zaptest.NewLogger(t)
	auditDir := t.TempDir()
	storeDir := t.TempDir()

	kern, err := kernel.GetInstance(logger, auditDir)
	if err != nil {
		t.Fatalf("kernel init: %v", err)
	}
	t.Cleanup(func() {
		kern.Shutdown()
		kernel.ResetInstance()
	})

	store, err := proposal.NewStore(storeDir, logger)
	if err != nil {
		t.Fatalf("proposal store init: %v", err)
	}

	ollamaHTTPClient := testutil.NewOllamaRecorderClient(t, "first-skill-tutorial-live")
	proxy := llm.NewOllamaProxyWithHTTPClient(
		llm.AllowedModelsFromRegistry(), "", ollamaHTTPClient, kern, logger,
	)

	// ── Load personas (create defaults if the user's config dir is absent) ──
	personaDir, err := court.EnsureDefaultPersonas(logger)
	if err != nil {
		t.Fatalf("ensure default personas: %v", err)
	}
	personas, err := court.LoadPersonas(personaDir, logger)
	if err != nil {
		t.Fatalf("load personas: %v", err)
	}
	t.Logf("✓ Loaded %d personas from %s", len(personas), personaDir)

	// ── Build court engine with InProcessSandboxLauncher ─────────────
	launcher := court.NewInProcessSandboxLauncher(proxy, logger)
	reviewer := court.NewReviewerWithLLMOptions(
		launcher, 2, logger,
		testutil.Float64(testutil.TestOllamaTemperature),
		testutil.TestOllamaSeed,
	)
	reviewerFn := court.NewReviewerFunc(reviewer)

	cfg := court.DefaultEngineConfig()
	engine, err := court.NewEngine(cfg, store, kern, personas, reviewerFn, logger, auditDir)
	if err != nil {
		t.Fatalf("court engine init: %v", err)
	}

	// ── Assemble a minimal runtimeEnv ────────────────────────────────
	env := &runtimeEnv{
		Logger:             logger,
		Kernel:             kern,
		ProposalStore:      store,
		Court:              engine,
		LLMProxy:           proxy,
		OllamaHTTPClient:   ollamaHTTPClient,
		TestLLMTemperature: testutil.Float64(testutil.TestOllamaTemperature),
		TestLLMSeed:        testutil.TestOllamaSeed,
	}

	// ── Create the draft proposal ─────────────────────────────────────
	t.Log("Creating draft proposal…")
	draftArgs := `{
		"title":       "InProcess e2e: time-of-day greeter skill",
		"description": "A minimal skill that returns a contextual greeting based on the time of day. Used in the inprocess end-to-end test.",
		"skill_name":  "time-of-day-greeter-inprocess",
		"tools": [
			{"name": "get_greeting", "description": "Returns a greeting string for the current time of day"}
		],
		"data_sensitivity": 1,
		"network_exposure":  1,
		"privilege_level":   1
	}`
	createResult, err := handleProposalCreateDraft(env, ctx, draftArgs)
	if err != nil {
		t.Fatalf("handleProposalCreateDraft: %v", err)
	}
	t.Logf("Draft created:\n%s", createResult)

	proposalID := ""
	for _, line := range strings.Split(createResult, "\n") {
		if strings.Contains(line, "ID:") {
			parts := strings.SplitN(line, "ID:", 2)
			if len(parts) == 2 {
				proposalID = strings.TrimSpace(parts[1])
			}
		}
	}
	if proposalID == "" {
		t.Fatal("could not parse proposal ID from handleProposalCreateDraft result")
	}
	t.Logf("✓ Draft proposal ID: %s", proposalID)

	// ── Submit + trigger court review (runs in-process, no VMs boot) ──
	t.Log("Submitting proposal and starting court review (in-process)…")
	submitArgs := fmt.Sprintf(`{"id": %q}`, proposalID)
	submitResult, err := handleProposalSubmitDirect(env, ctx, submitArgs)
	if err != nil {
		t.Fatalf("handleProposalSubmitDirect: %v", err)
	}
	t.Logf("Submit + review result:\n%s", submitResult)

	// ── Assert: court reached a terminal state ────────────────────────
	validTerminal := map[string]bool{
		string(court.SessionApproved):  true,
		string(court.SessionRejected):  true,
		string(court.SessionEscalated): true,
	}
	terminalFound := false
	for state := range validTerminal {
		if strings.Contains(submitResult, state) {
			terminalFound = true
			t.Logf("✓ Court reached terminal state: %s", state)
			break
		}
	}
	if !terminalFound {
		p, pErr := env.ProposalStore.Get(proposalID)
		if pErr != nil {
			t.Errorf("could not load proposal after review: %v", pErr)
		} else {
			t.Logf("Proposal status after review: %s  reviews=%d", p.Status, len(p.Reviews))
			terminal := p.Status == proposal.StatusApproved ||
				p.Status == proposal.StatusRejected ||
				p.Status == proposal.StatusInReview
			if !terminal {
				t.Errorf("proposal is in non-terminal state after full court review: %s", p.Status)
			} else {
				t.Logf("✓ Proposal in terminal state: %s", p.Status)
			}
		}
	}

	// ── Assert: audit log contains llm.infer entries ──────────────────
	// Confirms that InferDirect (and thus handleRequest) wrote audit entries,
	// proving the auditing path is exercised even without vsock/Firecracker.
	t.Log("Checking audit log for llm.infer entries…")
	logPath := env.Kernel.AuditLog().Path()
	entries, readErr := audit.ReadEntries(logPath)
	if readErr != nil {
		t.Errorf("audit.ReadEntries(%s): %v", logPath, readErr)
	} else {
		inferCount := 0
		for _, entry := range entries {
			var action struct {
				Action struct {
					Type string `json:"type"`
				} `json:"action"`
			}
			if json.Unmarshal(entry.Payload, &action) == nil &&
				action.Action.Type == "llm.infer" {
				inferCount++
			}
		}
		if inferCount == 0 {
			t.Error("no llm.infer entries in audit log — LLM inference was not invoked or not audited")
		} else {
			t.Logf("✓ Audit log contains %d llm.infer entries", inferCount)
		}
	}

	t.Log("TestFirstSkillTutorialInProcess PASSED")
}
