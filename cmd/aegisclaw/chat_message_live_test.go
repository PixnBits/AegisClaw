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
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/testutil"
)

var uuidInText = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func runChatMessageLiveScenario(t *testing.T, cassetteName, sessionID, input string, assertFn func(*testing.T, *runtimeEnv, api.ChatMessageResponse)) {
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

	callChat := func(t *testing.T, sessionID string, input string, history []api.ChatHistoryItem) api.ChatMessageResponse {
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
		return out
	}

	out := callChat(t, sessionID, input, nil)
	assertFn(t, env, out)

	if env.TaskExecutor == nil {
		t.Fatal("expected chat.message flow to initialize a TaskExecutor")
	}
	if _, ok := env.TaskExecutor.(*rtexec.FirecrackerTaskExecutor); !ok {
		t.Fatalf("expected FirecrackerTaskExecutor, got %T", env.TaskExecutor)
	}
}

func TestChatMessageLiveScenarioTimeQuestion(t *testing.T) {
	runChatMessageLiveScenario(
		t,
		"chat-message-time-live",
		"chat-live-time",
		"What time is it right now in Phoenix, AZ?",
		func(t *testing.T, _ *runtimeEnv, out api.ChatMessageResponse) {
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
		"Please create a proposal draft for a hello-world skill with one tool named greet that says hello.",
		func(t *testing.T, env *runtimeEnv, out api.ChatMessageResponse) {
			var toolCalls []map[string]interface{}
			if len(out.ToolCalls) > 0 {
				_ = json.Unmarshal(out.ToolCalls, &toolCalls)
			}
			if len(toolCalls) == 0 {
				t.Fatalf("expected at least one tool call in hello-world scenario; content=%q", out.Content)
			}

			id := uuidInText.FindString(out.Content)
			if strings.TrimSpace(id) == "" {
				t.Fatalf("expected response to include proposal UUID; response=%q", out.Content)
			}
			stored, err := env.ProposalStore.Get(id)
			if err != nil {
				t.Fatalf("expected created proposal %s in store: %v", id, err)
			}
			if stored.Title == "" {
				t.Errorf("expected stored proposal title to be non-empty for %s", id)
			}
			if !strings.Contains(strings.ToLower(string(stored.Spec)), "tools") {
				t.Errorf("expected stored proposal %s spec to include tools, got: %s", id, string(stored.Spec))
			}
		},
	)
}

func TestChatMessageLiveScenarioSolarSizing(t *testing.T) {
	runChatMessageLiveScenario(
		t,
		"chat-message-solar-live",
		"chat-live-solar",
		"Can you help me with a solar project? I'd like to put in a 20'x20' structure for shade and solar panels near my house here in Phoenix, AZ. What sort of yearly power generation could I expect if they're usual 350W 6'x3' panels? How many panels do I need to fully supply a 130kWh daily use in the hottest days of the summer?",
		func(t *testing.T, _ *runtimeEnv, out api.ChatMessageResponse) {
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
