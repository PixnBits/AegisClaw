package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"filippo.io/age"
	"github.com/PixnBits/AegisClaw/internal/composition"
	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/court"
	"github.com/PixnBits/AegisClaw/internal/eventbus"
	"github.com/PixnBits/AegisClaw/internal/events" // added for ProposalEventDispatcher
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/PixnBits/AegisClaw/internal/lookup"
	"github.com/PixnBits/AegisClaw/internal/memory"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	rtexec "github.com/PixnBits/AegisClaw/internal/runtime/exec"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"github.com/PixnBits/AegisClaw/internal/sessions"
	"github.com/PixnBits/AegisClaw/internal/vault"
	"github.com/PixnBits/AegisClaw/internal/worker"
	"github.com/PixnBits/AegisClaw/internal/workspace"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	runtimeOnce     sync.Once
	runtimeInst     *sandbox.FirecrackerRuntime
	registryInst    *sandbox.SkillRegistry
	proposalInst    *proposal.Store
	compositionInst *composition.Store
	memoryInst      *memory.Store
	eventBusInst    *eventbus.Bus
	workerStoreInst *worker.Store
	vaultInst       *vault.Vault
	lookupInst      *lookup.Store
	gitManagerInst  *gitmanager.Manager
	runtimeInitErr  error
)

type runtimeEnv struct {
	Logger             *zap.Logger
	Config             *config.Config
	Kernel             *kernel.Kernel
	Runtime            *sandbox.FirecrackerRuntime
	Registry           *sandbox.SkillRegistry
	ProposalStore      *proposal.Store
	CompositionStore   *composition.Store
	MemoryStore        *memory.Store
	EventBus           *eventbus.Bus
	WorkerStore        *worker.Store
	LookupStore        *lookup.Store
	Court              *court.Engine
	LLMProxy           *llm.OllamaProxy
	OllamaHTTPClient   *http.Client
	ToolEvents         *ToolEventBuffer
	ThoughtEvents      *ThoughtEventBuffer
	SafeMode           atomic.Bool
	TestLLMTemperature *float64
	TestLLMSeed        int64

	// TaskExecutor handles one turn of the agent ReAct loop.
	// The default (production) implementation is FirecrackerTaskExecutor which
	// routes calls through the Firecracker microVM.  Tests compiled with the
	// "inprocesstest" build tag may substitute InProcessTaskExecutor.
	// TaskExecutor is set lazily on the first chat.message request (alongside
	// AgentVMID) and is nil until then.
	TaskExecutor rtexec.TaskExecutor

	// Vault holds the age-encrypted secret store.  Opened once at daemon
	// startup; nil only if the vault directory could not be initialised
	// (daemon logs a warning and continues in degraded mode without secret
	// injection).
	Vault *vault.Vault

	// EgressProxy is the per-VM SNI-validating TCP tunnel proxy.  Started for
	// each skill VM whose approved proposal declares egress_mode "proxy".
	EgressProxy *llm.EgressProxy

	// Workspace holds content loaded from the user's workspace directory
	// (~/.aegisclaw/workspace by default). Fields are empty when the
	// corresponding workspace files are absent or the directory doesn't exist.
	Workspace *workspace.Content

	// GitManager manages the skills and self git repositories.
	GitManager *gitmanager.Manager

	// Sessions tracks all active and recent chat sessions for the session
	// routing tools (sessions_list, sessions_history, sessions_send,
	// sessions_spawn).  It is initialised once at daemon start and shared
	// across all API handler goroutines.
	Sessions *sessions.Store

	// AgentVMID is the ID of the main agent microVM. Protected by agentVMMu.
	// Set once by ensureAgentVM on the first chat.message request.
	AgentVMID string
	agentVMMu sync.Mutex

	// AegisHubVMID is the ID of the AegisHub system microVM launched at daemon
	// startup. AegisHub is the sole IPC router for the system; all inter-VM
	// traffic routes through it for ACL enforcement and audit logging.
	// The daemon registers it before starting any other VM.
	AegisHubVMID string

	// PortalVMID is the ID of the dashboard portal microVM. Protected by
	// portalVMMu and lazily started when dashboard.enabled is true.
	PortalVMID string
	portalVMMu sync.Mutex

	// ProposalEventDispatcher enables event-driven reactions to proposal lifecycle changes
	// (e.g. automatic builder pipeline trigger on "implementing" status).
	ProposalEventDispatcher *events.ProposalEventDispatcher

	// BuildOrchestrator coordinates automatic builder pipeline execution when proposals
	// reach implementing status after Court approval.
	BuildOrchestrator *builder.BuildOrchestrator
}

// ... (rest of file unchanged)
