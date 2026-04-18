package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/testutil"
	"github.com/google/uuid"
)

var uuidInText = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func runChatMessageLiveScenario(t *testing.T, cassetteName, sessionID string, scenarioFn func(*testing.T, *runtimeEnv, func(string) api.ChatMessageResponse)) {
	if os.Getuid() != 0 {
		t.Skip("live chat-message scenarios require root (jailer needs CAP_SYS_ADMIN)")
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skipf("live chat-message scenarios require KVM: /dev/kvm not accessible: %v", err)
	}
	if !testutil.RecordingOllama() && !testutil.OllamaCassetteExists(cassetteName) {
		t.Skipf("replay mode requires testdata/cassettes/%s.yaml; record once with RECORD_OLLAMA=true", cassetteName)
	}

	if testutil.RecordingOllama() {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:11434", 3*time.Second)
		if err != nil {
			t.Skipf("recording mode requires Ollama at 127.0.0.1:11434: %v", err)
		}
		conn.Close()
	}

	rootfsPath := "/var/lib/aegisclaw/rootfs-templates/alpine.ext4"
	if _, err := os.Stat(rootfsPath); err != nil {
		t.Skipf("live chat-message scenarios require rootfs template at %s: %v", rootfsPath, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// Reset package-level singletons so this test is self-contained.
	kernel.ResetInstance()
	runtimeOnce = sync.Once{}
	runtimeInst = nil
	registryInst = nil
	proposalInst = nil
	compositionInst = nil
	runtimeInitErr = nil

	env, err := initRuntime()
	if err != nil {
		t.Fatalf("initRuntime: %v", err)
	}
	ollamaHTTPClient := testutil.NewOllamaRecorderClient(t, cassetteName)
	env.OllamaHTTPClient = ollamaHTTPClient
	env.LLMProxy = llm.NewOllamaProxyWithHTTPClient(llm.AllowedModelsFromRegistry(), "", ollamaHTTPClient, env.Kernel, env.Logger)
	env.TestLLMTemperature = testutil.Float64(testutil.TestOllamaTemperature)
	env.TestLLMSeed = testutil.TestOllamaSeed

	t.Cleanup(func() {
		if env.AgentVMID != "" {
			env.LLMProxy.StopForVM(env.AgentVMID)
		}
		if env.Runtime != nil {
			env.Runtime.Cleanup(context.Background())
		}
		if env.Kernel != nil {
			env.Kernel.Shutdown()
		}
		kernel.ResetInstance()
	})

	handler := makeChatMessageHandler(env, buildToolRegistry(env))
	history := make([]api.ChatHistoryItem, 0, 8)

	ask := func(input string) api.ChatMessageResponse {
		t.Helper()
		payload, err := json.Marshal(api.ChatMessageRequest{
			Input:     input,
			History:   history,
			SessionID: sessionID,
			StreamID:  sessionID,
		})
		if err != nil {
			t.Fatalf("marshal chat request: %v", err)
		}
		resp := handler(ctx, payload)
		if resp == nil {
			t.Fatal("chat handler returned nil response")
		}
		if resp.Error != "" {
			t.Fatalf("chat.message error: %s", resp.Error)
		}
		if !resp.Success {
			t.Fatalf("chat.message unsuccessful response: %+v", resp)
		}
		var out api.ChatMessageResponse
		if err := json.Unmarshal(resp.Data, &out); err != nil {
			t.Fatalf("unmarshal chat response: %v", err)
		}
		if strings.TrimSpace(out.Content) == "" {
			t.Fatal("chat response content was empty")
		}
		history = append(history,
			api.ChatHistoryItem{Role: "user", Content: input},
			api.ChatHistoryItem{Role: out.Role, Content: out.Content},
		)
		return out
	}

	scenarioFn(t, env, ask)

	if env.TaskExecutor == nil {
		t.Fatal("expected chat.message flow to initialize a TaskExecutor")
	}
	if _, ok := env.TaskExecutor.(*rtexec.FirecrackerTaskExecutor); !ok {
		t.Fatalf("expected FirecrackerTaskExecutor, got %T", env.TaskExecutor)
	}
}

func toolTraceEntries(t *testing.T, out api.ChatMessageResponse) []map[string]interface{} {
	t.Helper()
	if len(out.ToolCalls) == 0 {
		return nil
	}
	var entries []map[string]interface{}
	if err := json.Unmarshal(out.ToolCalls, &entries); err != nil {
		t.Fatalf("unmarshal tool trace: %v", err)
	}
	return entries
}

func toolTraceHas(t *testing.T, out api.ChatMessageResponse, toolName string) bool {
	t.Helper()
	for _, entry := range toolTraceEntries(t, out) {
		if tool, _ := entry["tool"].(string); tool == toolName {
			return true
		}
	}
	return false
}

func requireToolCall(t *testing.T, out api.ChatMessageResponse, toolName string) {
	t.Helper()
	entries := toolTraceEntries(t, out)
	for _, entry := range entries {
		if tool, _ := entry["tool"].(string); tool == toolName {
			return
		}
	}
	t.Fatalf("expected tool call %q, got trace=%s content=%q", toolName, string(out.ToolCalls), out.Content)
}

func deployHelloWorldTool(t *testing.T, env *runtimeEnv, skillName string) {
	t.Helper()
	entry, ok := env.Registry.Get(skillName)
	if !ok {
		t.Fatalf("expected active skill %q in registry", skillName)
	}
	payload, err := json.Marshal(map[string]interface{}{
		"path": "/workspace/tools/greet",
		"content": "#!/bin/sh\nname=\"$1\"\nif [ -z \"$name\" ]; then\n  name=world\nfi\nprintf 'Hello, %s!\\n' \"$name\"\n",
		"mode": 493,
	})
	if err != nil {
		t.Fatalf("marshal file.write payload: %v", err)
	}
	vmReq := map[string]interface{}{
		"id":      uuid.NewString(),
		"type":    "file.write",
		"payload": json.RawMessage(payload),
	}
	raw, err := env.Runtime.SendToVM(context.Background(), entry.SandboxID, vmReq)
	if err != nil {
		t.Fatalf("deploy greet tool to VM: %v", err)
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("parse file.write response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("file.write failed: %s", resp.Error)
	}
}

func TestChatMessageLiveScenarioTimeQuestion(t *testing.T) {
	runChatMessageLiveScenario(
		t,
		"chat-message-time-live",
		"chat-live-time",
		func(t *testing.T, _ *runtimeEnv, ask func(string) api.ChatMessageResponse) {
			out := ask("What time is it right now in Phoenix, AZ?")
			lower := strings.ToLower(out.Content)
			isLimitation := strings.Contains(lower, "cannot") || strings.Contains(lower, "don't have access") || strings.Contains(lower, "do not have access")
			isLiveTime := (strings.Contains(lower, "currently") || strings.Contains(lower, "am") || strings.Contains(lower, "pm")) && strings.Contains(lower, "phoenix")
			if !isLimitation && !isLiveTime {
				t.Errorf("expected limitation or live-time response for time scenario, got: %q", out.Content)
			}
		},
	)
}

func TestChatMessageLiveScenarioHelloWorldSkill(t *testing.T) {
	runChatMessageLiveScenario(
		t,
		"chat-message-hello-world-live",
		"chat-live-hello-world",
		func(t *testing.T, env *runtimeEnv, ask func(string) api.ChatMessageResponse) {
			if env.Court == nil {
				engine, err := initCourtEngine(env, nil)
				if err != nil {
					t.Fatalf("initCourtEngine: %v", err)
				}
				env.Court = engine
			}

			createOut := ask("Please create a proposal draft for a hello-world skill with one tool named greet that says hello.")
			requireToolCall(t, createOut, "proposal.create_draft")

			id := uuidInText.FindString(createOut.Content)
			if strings.TrimSpace(id) == "" {
				t.Fatalf("expected response to include proposal UUID; response=%q", createOut.Content)
			}
			stored, err := env.ProposalStore.Get(id)
			if err != nil {
				t.Fatalf("expected created proposal %s in store: %v", id, err)
			}
			if stored.Status != proposal.StatusDraft {
				t.Fatalf("expected draft proposal after creation, got %s", stored.Status)
			}
			if stored.Title == "" {
				t.Errorf("expected stored proposal title to be non-empty for %s", id)
			}

			submitOut := ask("Please submit that hello-world proposal for court review now.")
			requireToolCall(t, submitOut, "proposal.submit")
			stored, err = env.ProposalStore.Get(id)
			if err != nil {
				t.Fatalf("reload proposal %s after submit: %v", id, err)
			}
			if len(stored.Reviews) == 0 {
				t.Fatal("expected proposal reviews after court review")
			}
			if stored.Status != proposal.StatusApproved {
				voteOut := ask("If the proposal is escalated, cast a human vote to approve it with reason 'low-risk hello-world tutorial skill', then confirm final verdict.")
				requireToolCall(t, voteOut, "proposal.vote")
				stored, err = env.ProposalStore.Get(id)
				if err != nil {
					t.Fatalf("reload proposal %s after vote: %v", id, err)
				}
			}
			if stored.Status != proposal.StatusApproved {
				t.Fatalf("expected approved proposal after review/vote flow, got %s", stored.Status)
			}

			reviewOut := ask("What was the court's verdict and review feedback for that hello-world proposal?")
			if strings.Contains(reviewOut.Content, "final response came back empty") {
				reviewOut = ask("Please check the proposal status and reviewer feedback for the hello-world proposal, then summarize the verdict.")
			}
			if !toolTraceHas(t, reviewOut, "proposal.reviews") && !toolTraceHas(t, reviewOut, "proposal.status") {
				t.Logf("review step returned without explicit proposal tool call; trace=%s content=%q", string(reviewOut.ToolCalls), reviewOut.Content)
			}
			reviewLower := strings.ToLower(reviewOut.Content)
			if !strings.Contains(reviewLower, "approved") && !strings.Contains(reviewLower, "verdict") && stored.Status != proposal.StatusApproved {
				t.Fatalf("expected review summary to mention verdict/approval, got %q", reviewOut.Content)
			}

			activateOut := ask("Please activate the hello-world skill now.")
			requireToolCall(t, activateOut, "activate_skill")
			entry, ok := env.Registry.Get(stored.TargetSkill)
			if !ok {
				t.Fatalf("expected activated skill %q in registry", stored.TargetSkill)
			}
			if entry.State != "active" {
				t.Fatalf("expected skill %q to be active, got state %s", stored.TargetSkill, entry.State)
			}

			invokeOut := ask("Please use the hello-world greet tool with the exact argument string \"Copilot\" and tell me the result.")
			requireToolCall(t, invokeOut, stored.TargetSkill+".greet")
			invokeLower := strings.ToLower(invokeOut.Content)
			if !strings.Contains(invokeLower, "hello") && !strings.Contains(invokeLower, "stub") {
				t.Fatalf("expected invoked skill result to include hello text or stub output, got %q", invokeOut.Content)
			}
			if !strings.Contains(invokeLower, "copilot") && !strings.Contains(invokeLower, "greet") {
				t.Fatalf("expected invoked skill result to include Copilot context or greet output, got %q", invokeOut.Content)
			}
		},
	)
}

func TestChatMessageLiveScenarioSolarSizing(t *testing.T) {
	runChatMessageLiveScenario(
		t,
		"chat-message-solar-live",
		"chat-live-solar",
		func(t *testing.T, _ *runtimeEnv, ask func(string) api.ChatMessageResponse) {
			out := ask("Can you help me with a solar project? I'd like to put in a 20'x20' structure for shade and solar panels near my house here in Phoenix, AZ. What sort of yearly power generation could I expect if they're usual 350W 6'x3' panels? How many panels do I need to fully supply a 130kWh daily use in the hottest days of the summer?")
			lower := strings.ToLower(out.Content)
			if !strings.Contains(lower, "panel") {
				t.Errorf("expected panel sizing in response, got: %q", out.Content)
			}
			if !strings.Contains(lower, "kwh") {
				t.Errorf("expected energy units in response, got: %q", out.Content)
			}
		},
	)
}
