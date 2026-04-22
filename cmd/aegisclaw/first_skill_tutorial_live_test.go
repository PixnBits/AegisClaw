//go:build livetest

// This file is excluded from the standard `go test ./...` run.
// It requires Firecracker, KVM, root privileges, and an alpine.ext4 rootfs template
// that are not available in standard GitHub Actions environments.
//
// To run this test locally:
//
//	sudo ./scripts/run-live-test.sh
//
// To regenerate the Ollama cassette:
//
//	RECORD_OLLAMA=true sudo ./scripts/run-live-test.sh
//
// The live-test CI job explicitly shows as "Skipped" on pull requests to signal
// that it requires special infrastructure — it is NOT silently passing.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
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
)

// TestFirstSkillTutorialLive is a real end-to-end test that:
//   - Boots actual Firecracker microVMs via the jailer (requires root + KVM)
//   - Routes LLM inference through the vsock OllamaProxy to a running Ollama daemon
//   - Runs the full court review pipeline with all 5 personas
//   - Verifies the audit log recorded llm.infer entries
//
// Prerequisites (test fails fast when missing — this test is only compiled with
// -tags=livetest, so these are hard requirements, not soft skips):
//
//	root             – jailer requires elevated privileges
//	/dev/kvm         – hardware virtualization acceleration
//	Ollama at :11434 – mistral-nemo:latest + llama3.2:3b must be available
//	alpine.ext4      – rootfs template with vsock-proxy guest-agent installed
//
// Budget: 15 minutes — Firecracker boot (~5 s) × up to 5 VMs + LLM inference.
func TestFirstSkillTutorialLive(t *testing.T) {
	recording := testutil.RecordingOllama()

	// ── Prerequisite: must run as root ────────────────────────────────
	if os.Getuid() != 0 {
		t.Fatalf("TestFirstSkillTutorialLive requires root (jailer needs CAP_SYS_ADMIN)")
	}

	// ── Prerequisite: KVM must be accessible ─────────────────────────
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Fatalf("TestFirstSkillTutorialLive requires KVM: /dev/kvm not accessible: %v", err)
	}
	t.Log("✓ /dev/kvm accessible")
	if !recording && !testutil.OllamaCassetteExists("first-skill-tutorial-live") {
		t.Fatalf("TestFirstSkillTutorialLive replay mode requires testdata/cassettes/first-skill-tutorial-live.yaml; record it once with RECORD_OLLAMA=true")
	}

	// ── Prerequisite: live Ollama only when refreshing cassettes ──────
	if recording {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:11434", 3*time.Second)
		if err != nil {
			t.Fatalf("TestFirstSkillTutorialLive recording mode requires Ollama: cannot reach 127.0.0.1:11434 — start Ollama and ensure mistral-nemo:latest and llama3.2:3b are available: %v", err)
		}
		conn.Close()
		t.Log("✓ Ollama reachable at :11434 (recording mode)")
	} else {
		t.Log("✓ Ollama cassette replay mode active")
	}

	// ── Prerequisite: alpine.ext4 rootfs template must exist ─────────
	rootfsPath := "/var/lib/aegisclaw/rootfs-templates/alpine.ext4"
	if _, err := os.Stat(rootfsPath); err != nil {
		t.Fatalf("TestFirstSkillTutorialLive requires rootfs: %s not found: %v", rootfsPath, err)
	}
	t.Logf("✓ rootfs template at %s", rootfsPath)

	// ── Context with 15-minute deadline ──────────────────────────────
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// ── Reset all package-level singletons ───────────────────────────
	// Each test in this package may have mutated global state.  Reset
	// before initialising the real runtime.
	kernel.ResetInstance()
	runtimeOnce = sync.Once{}
	runtimeInst = nil
	registryInst = nil
	proposalInst = nil
	compositionInst = nil
	runtimeInitErr = nil

	// ── Initialise the real runtime ───────────────────────────────────
	t.Log("Initialising runtime from config…")
	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	ollamaHTTPClient := testutil.NewOllamaRecorderClient(t, "first-skill-tutorial-live")
	env.OllamaHTTPClient = ollamaHTTPClient
	env.LLMProxy = llm.NewOllamaProxyWithHTTPClient(llm.AllowedModelsFromRegistry(), "", ollamaHTTPClient, env.Kernel, env.Logger)
	env.TestLLMTemperature = testutil.Float64(testutil.TestOllamaTemperature)
	env.TestLLMSeed = testutil.TestOllamaSeed
	t.Logf("✓ Runtime ready  kernel=%p  config=%p", env.Kernel, env.Config)
	t.Logf("  firecracker=%s  jailer=%s", env.Config.Firecracker.Bin, env.Config.Jailer.Bin)
	t.Logf("  kernel_image=%s", env.Config.Sandbox.KernelImage)
	t.Logf("  rootfs_template=%s", env.Config.Rootfs.Template)

	// ── Initialise the real Court engine ─────────────────────────────
	t.Log("Initialising court engine (FirecrackerLauncher + OllamaProxy)…")
	engine, err := initCourtEngine(env, nil)
	if err != nil {
		t.Fatalf("initCourtEngine: %v", err)
	}
	env.Court = engine
	t.Log("✓ Court engine ready")

	// ── Create the draft proposal ─────────────────────────────────────
	t.Log("Creating draft proposal…")
	draftArgs := `{
		"title":       "Live e2e: time-of-day greeter skill",
		"description": "A minimal skill that returns a contextual greeting based on the time of day. Used in the live end-to-end test.",
		"skill_name":  "time-of-day-greeter-live",
		"tools": [
			{"name": "get_greeting", "description": "Returns a greeting string for the current time of day"}
		],
		"data_sensitivity": 1,
		"network_exposure":  1,
		"privilege_level":   1
	}`
	createResult, err := handleProposalCreateDraft(env, context.Background(), draftArgs)
	if err != nil {
		t.Fatalf("handleProposalCreateDraft: %v", err)
	}
	t.Logf("Draft created:\n%s", createResult)

	// Parse the proposal ID from the result string.
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

	// ── Submit + trigger real court review ────────────────────────────
	// handleProposalSubmitDirect transitions draft→submitted and calls
	// env.Court.Review inline.  This is where Firecracker VMs boot.
	t.Log("Submitting proposal and starting court review…")
	t.Log("  (Firecracker VMs will boot now; this may take several minutes)")
	submitArgs := fmt.Sprintf(`{"id": %q}`, proposalID)
	submitResult, err := handleProposalSubmitDirect(env, ctx, submitArgs)
	if err != nil {
		t.Fatalf("handleProposalSubmitDirect: %v", err)
	}
	t.Logf("Submit + review result:\n%s", submitResult)

	// ── Assert: court reached a terminal state ────────────────────────
	// All three terminal states are valid real-world outcomes.
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
		// Fall back to checking the session directly.
		p, pErr := env.ProposalStore.Get(proposalID)
		if pErr != nil {
			t.Errorf("could not load proposal after review: %v", pErr)
		} else {
			t.Logf("Proposal status after review: %s  reviews=%d", p.Status, len(p.Reviews))
			// Court escalation maps to inReview (pending human tiebreak); approved and
			// rejected are the decisive outcomes.  All three mean the engine ran to
			// completion — not stuck in "submitted".
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
	// This proves reviewer inference ran through the vsock proxy even when the
	// underlying Ollama HTTP responses were replayed from cassettes.
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

	t.Log("TestFirstSkillTutorialLive PASSED")
}
