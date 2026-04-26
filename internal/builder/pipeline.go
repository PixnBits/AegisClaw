package builder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/builder/securitygate"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/sbom"
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
	ProposalID         string                       `json:"proposal_id"`
	State              PipelineState                `json:"state"`
	BuilderID          string                       `json:"builder_id"`
	CommitHash         string                       `json:"commit_hash"`
	Branch             string                       `json:"branch"`
	Diff               string                       `json:"diff"`
	Files              map[string]string            `json:"files"`
	FileHashes         map[string]string            `json:"file_hashes"`
	Reasoning          string                       `json:"reasoning"`
	Analysis           *AnalysisResult              `json:"analysis,omitempty"`
	SecurityGateResult *securitygate.PipelineResult `json:"security_gate_result,omitempty"`
	// SBOMPath is the filesystem path to the emitted sbom.json for this build.
	// Empty if SBOM generation was skipped (e.g. no output dir configured).
	SBOMPath    string        `json:"sbom_path,omitempty"`
	Error       string        `json:"error,omitempty"`
	Round       int           `json:"round"`
	StartedAt   time.Time     `json:"started_at"`
	CompletedAt time.Time     `json:"completed_at,omitempty"`
	Duration    time.Duration `json:"duration,omitempty"`
}

// Pipeline orchestrates the end-to-end flow from approved proposal to code diff.
// It coordinates BuilderRuntime, CodeGenerator, GitManager, and Analyzer.
type Pipeline struct {
	builderRT BuilderRuntimeInterface
	codeGen   *CodeGenerator
	gitMgr    *gitmanager.Manager
	analyzer  *Analyzer
	kern      *kernel.Kernel
	store     *proposal.Store
	logger    *zap.Logger
	// sbomDir, when non-empty, enables SBOM generation.  The sbom.json file is
	// written to <sbomDir>/<proposalID>/ after a successful build.
	sbomDir string
	// workspaceSkillContext is the content of SKILL.md from the user's
	// workspace directory.  When non-empty it is appended to the system prompt
	// for each code-generation round so users can inject project-specific
	// context without modifying Court-reviewed templates.
	workspaceSkillContext string
	// onPRCreated is an optional callback invoked after a PR is auto-created.
	// This allows external systems (e.g., the daemon) to trigger follow-up
	// actions like Court code review.
	// Parameters: proposalID, branch, commitHash, pipelineResult
	onPRCreated func(proposalID, branch, commitHash string, result *PipelineResult)
	mu          sync.Mutex
	runs        map[string]*PipelineResult
}

// NewPipeline creates a Pipeline connecting all subsystems.
func NewPipeline(
	br BuilderRuntimeInterface,
	cg *CodeGenerator,
	gm *gitmanager.Manager,
	az *Analyzer,
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
		builderRT:             br,
		codeGen:               cg,
		gitMgr:                gm,
		analyzer:              az,
		kern:                  kern,
		store:                 store,
		logger:                logger,
		runs:                  make(map[string]*PipelineResult),
		workspaceSkillContext: "",
		onPRCreated:           nil,
	}, nil
}

// SetPRCreatedCallback sets a callback to be invoked after a PR is auto-created.
// The callback receives: proposalID, branch, commitHash, and the pipeline result.
func (p *Pipeline) SetPRCreatedCallback(cb func(proposalID, branch, commitHash string, result *PipelineResult)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onPRCreated = cb
}

// SetSBOMDir configures the directory where SBOM JSON files are written.
// Call this before Execute; an empty string disables SBOM generation.
func (p *Pipeline) SetSBOMDir(dir string) {
	p.mu.Lock()
	p.sbomDir = dir
	p.mu.Unlock()
}

// SetWorkspaceSkillContext sets the SKILL.md content from the user's workspace
// directory.  When non-empty, it is appended to the code-generation system
// prompt so project-specific context is available to the Builder without
// modifying Court-reviewed templates.  Call this before Execute.
func (p *Pipeline) SetWorkspaceSkillContext(ctx string) {
	p.mu.Lock()
	p.workspaceSkillContext = ctx
	p.mu.Unlock()
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
	if !prop.IsApproved() {
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
	} else if isScriptingLanguage(spec.Language) {
		templateName = "skill_script_runner"
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
		Spec:                  *spec,
		ExistingCode:          existingCode,
		Round:                 1,
		SystemPrompt:          systemPrompt,
		MaxTokens:             8192,
		WorkspaceSkillContext: p.workspaceSkillContext,
	}

	codeResp, err := p.codeGen.Generate(ctx, builderInfo.ID, codeReq)
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

	// Step 8: Run static analysis and build inside builder sandbox
	if p.analyzer != nil {
		analysisReq := &AnalysisRequest{
			ProposalID: prop.ID,
			Files:      codeResp.Files,
			Diff:       diff,
			SkillName:  spec.Name,
		}

		analysisResult, analysisErr := p.analyzer.Analyze(builderInfo.ID, analysisReq)
		if analysisErr != nil {
			p.logger.Error("analysis failed", zap.Error(analysisErr))
			result.State = PipelineStateFailed
			result.Error = fmt.Sprintf("analysis failed: %v", analysisErr)
			return result, fmt.Errorf("pipeline: %s", result.Error)
		}

		result.Analysis = analysisResult

		// Fail pipeline on high-severity findings
		if !analysisResult.Passed {
			result.State = PipelineStateFailed
			reason := "analysis did not pass"
			if analysisResult.FailureReason != "" {
				reason = analysisResult.FailureReason
			}
			result.Error = reason
			result.CompletedAt = time.Now().UTC()
			result.Duration = result.CompletedAt.Sub(result.StartedAt)
			return result, fmt.Errorf("pipeline: %s", reason)
		}

		// Record binary hash if available
		if analysisResult.BinaryHash != "" {
			result.FileHashes["_binary"] = analysisResult.BinaryHash
		}
	}

	// Step 8.5 (D8): Run security gates — SAST, SCA, secrets scanning, and
	// policy-as-code evaluation. These gates are mandatory and cannot be
	// bypassed even if the analyzer step passes.
	sgPipeline := securitygate.DefaultPipeline(securitygate.DefaultPolicies())
	sgReq := &securitygate.EvalRequest{
		ProposalID: prop.ID,
		SkillName:  spec.Name,
		Files:      codeResp.Files,
		Diff:       diff,
	}
	sgResult, sgErr := sgPipeline.Evaluate(sgReq)
	if sgErr != nil {
		p.logger.Error("security gate evaluation failed", zap.Error(sgErr))
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("security gate evaluation failed: %v", sgErr)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}
	result.SecurityGateResult = sgResult

	if !sgResult.Passed {
		p.logger.Warn("security gates blocked pipeline",
			zap.String("proposal_id", prop.ID),
			zap.Int("blocking_findings", sgResult.BlockingFindings),
			zap.Int("total_findings", sgResult.TotalFindings),
		)
		result.State = PipelineStateFailed
		result.Error = fmt.Sprintf("security gates failed: %d blocking findings out of %d total",
			sgResult.BlockingFindings, sgResult.TotalFindings)
		result.CompletedAt = time.Now().UTC()
		result.Duration = result.CompletedAt.Sub(result.StartedAt)
		return result, fmt.Errorf("pipeline: %s", result.Error)
	}

	// Step 9.5: Emit SBOM if configured.
	p.mu.Lock()
	sbomDir := p.sbomDir
	p.mu.Unlock()
	if sbomDir != "" {
		version := commitHash
		if len(version) > 12 {
			version = version[:12]
		}
		s := sbom.Generate(sbom.BuildInfo{
			SkillName:        spec.Name,
			SkillDescription: spec.Description,
			Version:          version,
			Language:         spec.Language,
			Files:            codeResp.Files,
			FileHashes:       result.FileHashes,
			ProposalID:       prop.ID,
		})
		destDir := filepath.Join(sbomDir, prop.ID)
		if sbomPath, sbomErr := sbom.Write(destDir, s); sbomErr != nil {
			p.logger.Warn("SBOM write failed (non-fatal)", zap.Error(sbomErr))
		} else {
			result.SBOMPath = sbomPath
			p.logger.Info("SBOM written", zap.String("path", sbomPath))
		}
	}

	// Step 9: Mark complete
	result.State = PipelineStateComplete
	result.CompletedAt = time.Now().UTC()
	result.Duration = result.CompletedAt.Sub(result.StartedAt)

	// Audit log the pipeline completion
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"proposal_id": prop.ID,
		"commit":      commitHash,
		"branch":      result.Branch,
		"files":       len(codeResp.Files),
		"analysis":    result.Analysis != nil,
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

	// Step 10: Auto-create pull request (Phase 4)
	// Call the PR creation callback if configured
	p.mu.Lock()
	callback := p.onPRCreated
	p.mu.Unlock()

	if callback != nil {
		// Callback will create PR and trigger Court code review
		callback(result.ProposalID, result.Branch, commitHash, result)
	}

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
