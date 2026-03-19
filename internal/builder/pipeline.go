package builder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"go.uber.org/zap"
)

// PipelineState represents the lifecycle of a build pipeline run.
type PipelineState string

const (
	PipelineStatePending   PipelineState = "pending"
	PipelineStateBuilding  PipelineState = "building"
	PipelineStateComplete  PipelineState = "complete"
	PipelineStateFailed    PipelineState = "failed"
	PipelineStateCancelled PipelineState = "cancelled"
)

// PipelineResult captures the outcome of a pipeline run.
type PipelineResult struct {
	ProposalID   string            `json:"proposal_id"`
	State        PipelineState     `json:"state"`
	BuilderID    string            `json:"builder_id"`
	CommitHash   string            `json:"commit_hash"`
	Branch       string            `json:"branch"`
	Diff         string            `json:"diff"`
	Files        map[string]string `json:"files"`
	FileHashes   map[string]string `json:"file_hashes"`
	Reasoning    string            `json:"reasoning"`
	Error        string            `json:"error,omitempty"`
	Round        int               `json:"round"`
	StartedAt    time.Time         `json:"started_at"`
	CompletedAt  time.Time         `json:"completed_at,omitempty"`
	Duration     time.Duration     `json:"duration,omitempty"`
}

// Pipeline orchestrates the end-to-end flow from approved proposal to code diff.
// It coordinates BuilderRuntime, CodeGenerator, and GitManager.
type Pipeline struct {
	builderRT  *BuilderRuntime
	codeGen    *CodeGenerator
	gitMgr     *gitmanager.Manager
	kern       *kernel.Kernel
	store      *proposal.Store
	logger     *zap.Logger
	mu         sync.Mutex
	runs       map[string]*PipelineResult
}

// NewPipeline creates a Pipeline connecting all subsystems.
func NewPipeline(
	br *BuilderRuntime,
	cg *CodeGenerator,
	gm *gitmanager.Manager,
	kern *kernel.Kernel,
	store *proposal.Store,
	logger *zap.Logger,
) (*Pipeline, error) {
	if br == nil {
		return nil, fmt.Errorf("builder runtime is required")
	}
	if cg == nil {
		return nil, fmt.Errorf("code generator is required")
	}
	if gm == nil {
		return nil, fmt.Errorf("git manager is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}
	if store == nil {
		return nil, fmt.Errorf("proposal store is required")
	}

	return &Pipeline{
		builderRT: br,
		codeGen:   cg,
		gitMgr:    gm,
		kern:      kern,
		store:     store,
		logger:    logger,
		runs:      make(map[string]*PipelineResult),
	}, nil
}

// Execute runs the full pipeline for an approved proposal.
// Flow: Launch builder → generate code → create branch → commit → diff → return result.
func (p *Pipeline) Execute(ctx context.Context, prop *proposal.Proposal, spec *SkillSpec) (*PipelineResult, error) {
	if prop == nil {
		return nil, fmt.Errorf("proposal is required")
	}
	if spec == nil {
		return nil, fmt.Errorf("skill spec is required")
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid skill spec: %w", err)
	}
	if prop.Status != proposal.StatusApproved && prop.Status != proposal.StatusImplementing {
		return nil, fmt.Errorf("proposal must be approved or implementing, got %s", prop.Status)
	}

	result := &PipelineResult{
		ProposalID: prop.ID,
		State:      PipelineStatePending,
		StartedAt:  time.Now().UTC(),
		Round:      1,
	}

	p.mu.Lock()
	p.runs[prop.ID] = result
	p.mu.Unlock()

	// Step 1: Determine repo kind
	repoKind := gitmanager.RepoSkills
	if prop.Category == proposal.CategoryKernelPatch {
		repoKind = gitmanager.RepoSelf
	}

	// Step 2: Launch builder sandbox
	result.State = PipelineStateBuilding
	builderSpec := DefaultBuilderSpec(prop.ID)
	builderInfo, err := p.builderRT.LaunchBuilder(ctx, builderSpec)
	if err != nil {
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("failed to launch builder: %v", err)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}
	result.BuilderID = builderInfo.ID

	defer func() {
		if stopErr := p.builderRT.StopBuilder(ctx, builderInfo.ID); stopErr != nil {
			p.logger.Error("pipeline: failed to stop builder",
				zap.String("builder_id", builderInfo.ID),
				zap.Error(stopErr),
			)
		}
	}()

	// Step 3: Collect existing code for edit mode
	var existingCode map[string]string
	if prop.Category == proposal.CategoryEditSkill {
		existingCode, err = p.collectExistingCode(repoKind, spec.Name)
		if err != nil {
			p.logger.Warn("failed to collect existing code, proceeding as new skill",
				zap.String("skill", spec.Name),
				zap.Error(err),
			)
		}
	}

	// Step 4: Generate code via CodeGenerator
	templateName := "skill_codegen"
	if prop.Category == proposal.CategoryEditSkill && len(existingCode) > 0 {
		templateName = "skill_edit"
	}

	tmpl, ok := p.codeGen.GetTemplate(templateName)
	if !ok {
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("template %q not found", templateName)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}

	specJSON, _ := json.Marshal(spec)
	systemPrompt, _ := tmpl.Format(map[string]string{
		"skill_spec": string(specJSON),
	})

	codeReq := &CodeGenRequest{
		Spec:         *spec,
		ExistingCode: existingCode,
		Round:        1,
		SystemPrompt: systemPrompt,
		MaxTokens:    8192,
	}

	codeResp, err := p.codeGen.Generate(builderInfo.ID, codeReq)
	if err != nil {
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("code generation failed: %v", err)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}

	result.Files = codeResp.Files
	result.Reasoning = codeResp.Reasoning

	// Step 5: Create git branch and commit
	if err := p.gitMgr.CreateProposalBranch(repoKind, prop.ID); err != nil {
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("failed to create branch: %v", err)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}

	result.Branch = "proposal-" + prop.ID

	commitMsg := fmt.Sprintf("feat(%s): %s\n\nProposal: %s\nGenerated by AegisClaw builder pipeline",
		spec.Name, prop.Title, prop.ID)

	commitHash, err := p.gitMgr.CommitFiles(repoKind, codeResp.Files, commitMsg)
	if err != nil {
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("failed to commit: %v", err)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}
	result.CommitHash = commitHash

	// Step 6: Generate diff for Court review
	diff, err := p.gitMgr.GenerateDiff(repoKind, prop.ID)
	if err != nil {
		p.logger.Warn("failed to generate diff", zap.Error(err))
		diff = "(diff generation failed)"
	}
	result.Diff = diff

	// Step 7: Compute file hashes for integrity
	result.FileHashes = computeFileHashes(codeResp.Files)

	// Step 8: Mark complete
	result.State = PipelineStateComplete
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// Audit log the pipeline completion
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": prop.ID,
		"commit":      commitHash,
		"branch":      result.Branch,
		"files":       len(codeResp.Files),
		"duration":    result.Duration.String(),
	})
	action := kernel.NewAction(kernel.ActionBuilderBuild, "pipeline", auditPayload)
	if _, logErr := p.kern.SignAndLog(action); logErr != nil {
		p.logger.Error("failed to log pipeline completion", zap.Error(logErr))
	}

	p.logger.Info("pipeline complete",
		zap.String("proposal_id", prop.ID),
		zap.String("commit", commitHash[:12]),
		zap.String("branch", result.Branch),
		zap.Int("files", len(codeResp.Files)),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

// collectExistingCode reads files from the skill directory in the repo.
func (p *Pipeline) collectExistingCode(repoKind gitmanager.RepoKind, skillName string) (map[string]string, error) {
	// For edit mode, we need to read existing files from the main branch.
	// The git manager doesn't expose raw file reading, so we return nil here.
	// The builder sandbox will receive existing code via the proposal spec.
	return nil, nil
}

// computeFileHashes returns SHA-256 hex hashes for each file.
func computeFileHashes(files map[string]string) map[string]string {
	hashes := make(map[string]string, len(files))
	for path, content := range files {
		sum := sha256.Sum256([]byte(content))
		hashes[path] = fmt.Sprintf("%x", sum)
	}
	return hashes
}

// GetResult returns the pipeline result for a proposal.
func (p *Pipeline) GetResult(proposalID string) (*PipelineResult, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.runs[proposalID]
	return r, ok
}

// ListResults returns all pipeline results.
func (p *Pipeline) ListResults() []*PipelineResult {
	p.mu.Lock()
	defer p.mu.Unlock()

	results := make([]*PipelineResult, 0, len(p.runs))
	for _, r := range p.runs {
		results = append(results, r)
	}
	return results
}
