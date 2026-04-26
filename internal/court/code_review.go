package court

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/pullrequest"
	"go.uber.org/zap"
)

// CodeReviewRequest contains the context for reviewing a code change.
type CodeReviewRequest struct {
	PRID        string                       `json:"pr_id"`
	ProposalID  string                       `json:"proposal_id"`
	Title       string                       `json:"title"`
	Description string                       `json:"description"`
	Branch      string                       `json:"branch"`
	CommitHash  string                       `json:"commit_hash"`
	Files       map[string]string            `json:"files"`         // filepath -> content
	FilesChanged int                         `json:"files_changed"`
	Additions   int                          `json:"additions"`
	Deletions   int                          `json:"deletions"`
	BuildPassed bool                         `json:"build_passed"`
	AnalysisPassed bool                      `json:"analysis_passed"`
	SecurityGatesPassed bool                 `json:"security_gates_passed"`
}

// Validate ensures the code review request is safe and well-formed.
func (crr *CodeReviewRequest) Validate() error {
	if crr.PRID == "" {
		return fmt.Errorf("PR ID is required")
	}
	if crr.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if crr.Title == "" {
		return fmt.Errorf("title is required")
	}
	if crr.CommitHash == "" {
		return fmt.Errorf("commit hash is required")
	}
	
	// Security: Limit the number of files to prevent DoS
	const maxFiles = 100
	if len(crr.Files) > maxFiles {
		return fmt.Errorf("too many files (%d > %d)", len(crr.Files), maxFiles)
	}
	
	// Security: Validate file paths for path traversal
	for path := range crr.Files {
		if strings.Contains(path, "..") {
			return fmt.Errorf("invalid file path: %q (contains ..)", path)
		}
		if strings.HasPrefix(path, "/") {
			return fmt.Errorf("invalid file path: %q (absolute paths not allowed)", path)
		}
	}
	
	// Security: Limit total code size to prevent memory exhaustion
	const maxTotalSize = 10 * 1024 * 1024 // 10MB
	totalSize := 0
	for _, content := range crr.Files {
		totalSize += len(content)
		if totalSize > maxTotalSize {
			return fmt.Errorf("total code size exceeds limit (%d > %d bytes)", totalSize, maxTotalSize)
		}
	}
	
	return nil
}

// ReviewCode starts a court review session for generated code changes.
// This is separate from proposal review - it focuses on implementation quality,
// security vulnerabilities, and compliance with best practices.
func (e *Engine) ReviewCode(ctx context.Context, req *CodeReviewRequest) ([]pullrequest.CourtReview, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("invalid code review request: %w", err)
	}
	
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Log the code review action to kernel audit trail
	payload, _ := json.Marshal(map[string]interface{}{
		"pr_id":        req.PRID,
		"proposal_id":  req.ProposalID,
		"commit_hash":  req.CommitHash,
		"files_count":  len(req.Files),
		"additions":    req.Additions,
		"deletions":    req.Deletions,
	})
	action := kernel.NewAction(kernel.ActionType("code.review"), "court-engine", payload)
	if _, err := e.kernel.SignAndLog(action); err != nil {
		return nil, fmt.Errorf("failed to log code review action: %w", err)
	}
	
	e.logger.Info("starting code review",
		zap.String("pr_id", req.PRID),
		zap.String("proposal_id", req.ProposalID),
		zap.Int("files", len(req.Files)),
		zap.Int("personas", len(e.personas)),
	)
	
	// Run code review with each persona
	reviews := make([]pullrequest.CourtReview, 0, len(e.personas))
	var reviewErrors []string
	
	for _, persona := range e.personas {
		review, err := e.runCodeReviewWithPersona(ctx, req, persona)
		if err != nil {
			e.logger.Error("code review failed for persona",
				zap.String("persona", persona.Name),
				zap.Error(err),
			)
			reviewErrors = append(reviewErrors, fmt.Sprintf("%s: %v", persona.Name, err))
			continue
		}
		
		reviews = append(reviews, *review)
		
		// Log individual review
		reviewPayload, _ := json.Marshal(map[string]interface{}{
			"pr_id":      req.PRID,
			"persona":    persona.Name,
			"verdict":    review.Verdict,
			"risk_score": review.RiskScore,
		})
		action := kernel.NewAction(kernel.ActionType("code.review.persona"), persona.Name, reviewPayload)
		if _, err := e.kernel.SignAndLog(action); err != nil {
			e.logger.Warn("failed to log persona review", zap.Error(err))
		}
	}
	
	// Require at least half of personas to succeed
	minRequired := (len(e.personas) + 1) / 2
	if len(reviews) < minRequired {
		return nil, fmt.Errorf("insufficient reviews (%d/%d succeeded): %s",
			len(reviews), len(e.personas), strings.Join(reviewErrors, "; "))
	}
	
	e.logger.Info("code review completed",
		zap.String("pr_id", req.PRID),
		zap.Int("reviews", len(reviews)),
		zap.Int("personas", len(e.personas)),
	)
	
	return reviews, nil
}

// runCodeReviewWithPersona executes a single code review with a specific persona.
func (e *Engine) runCodeReviewWithPersona(ctx context.Context, req *CodeReviewRequest, persona *Persona) (*pullrequest.CourtReview, error) {
	// Build the code review prompt
	prompt := buildCodeReviewPrompt(req, persona)
	
	// Execute the review using the reviewer function
	// Convert code request to a minimal proposal for the reviewer
	pseudoProposal := convertCodeReqToProposal(req)
	// Add the full code context as the description
	pseudoProposal.Description = prompt
	
	llmReview, err := e.reviewerFn(ctx, pseudoProposal, persona)
	if err != nil {
		return nil, fmt.Errorf("reviewer execution failed: %w", err)
	}
	
	// Convert to CourtReview format
	courtReview := &pullrequest.CourtReview{
		ID:        generateReviewID(),
		Persona:   persona.Name,
		Verdict:   string(llmReview.Verdict),
		RiskScore: llmReview.RiskScore,
		Comments:  llmReview.Comments,
		Timestamp: llmReview.Timestamp,
	}
	
	// Extract inline comments from evidence
	if len(llmReview.Evidence) > 0 {
		courtReview.InlineComments = extractInlineComments(llmReview.Evidence, req.Files)
	}
	
	return courtReview, nil
}

// buildCodeReviewPrompt creates a specialized prompt for code review.
func buildCodeReviewPrompt(req *CodeReviewRequest, persona *Persona) string {
	var sb strings.Builder
	
	sb.WriteString("# Code Review Request\n\n")
	sb.WriteString(fmt.Sprintf("**PR Title:** %s\n", req.Title))
	sb.WriteString(fmt.Sprintf("**Proposal:** %s\n", req.ProposalID))
	sb.WriteString(fmt.Sprintf("**Commit:** %s\n", req.CommitHash))
	sb.WriteString(fmt.Sprintf("**Branch:** %s\n\n", req.Branch))
	
	if req.Description != "" {
		sb.WriteString("## Description\n\n")
		sb.WriteString(req.Description)
		sb.WriteString("\n\n")
	}
	
	sb.WriteString("## Build & Security Status\n\n")
	sb.WriteString(fmt.Sprintf("- Build: %s\n", boolToStatus(req.BuildPassed)))
	sb.WriteString(fmt.Sprintf("- Analysis: %s\n", boolToStatus(req.AnalysisPassed)))
	sb.WriteString(fmt.Sprintf("- Security Gates: %s\n\n", boolToStatus(req.SecurityGatesPassed)))
	
	sb.WriteString("## Code Changes\n\n")
	sb.WriteString(fmt.Sprintf("- Files Changed: %d\n", req.FilesChanged))
	sb.WriteString(fmt.Sprintf("- Additions: +%d\n", req.Additions))
	sb.WriteString(fmt.Sprintf("- Deletions: -%d\n\n", req.Deletions))
	
	sb.WriteString("## Files\n\n")
	
	// List files first (table of contents)
	fileList := make([]string, 0, len(req.Files))
	for path := range req.Files {
		fileList = append(fileList, path)
	}
	// Sort for deterministic output
	sortStrings(fileList)
	
	for _, path := range fileList {
		sb.WriteString(fmt.Sprintf("- `%s`\n", path))
	}
	sb.WriteString("\n")
	
	// Include file contents (limited to prevent prompt size explosion)
	const maxFilesToInclude = 20
	included := 0
	for _, path := range fileList {
		if included >= maxFilesToInclude {
			remaining := len(fileList) - included
			sb.WriteString(fmt.Sprintf("\n... and %d more files (truncated for review)\n", remaining))
			break
		}
		
		content := req.Files[path]
		sb.WriteString(fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", path, content))
		included++
	}
	
	sb.WriteString("## Review Guidelines\n\n")
	sb.WriteString(fmt.Sprintf("As a %s, please review this code for:\n\n", persona.Role))
	sb.WriteString("1. **Security vulnerabilities** - Look for injection attacks, path traversal, insecure dependencies\n")
	sb.WriteString("2. **Code quality** - Check for proper error handling, edge cases, resource leaks\n")
	sb.WriteString("3. **Best practices** - Verify idiomatic usage, documentation, test coverage\n")
	sb.WriteString("4. **Logic errors** - Identify bugs, race conditions, incorrect algorithms\n")
	sb.WriteString("5. **Compliance** - Ensure adherence to project standards and security policies\n\n")
	
	sb.WriteString("Provide your verdict (approve/reject/ask) and risk assessment (0-10).\n")
	sb.WriteString("Include specific file/line references in your evidence.\n")
	
	return sb.String()
}

// convertCodeReqToProposal creates a minimal proposal object for the reviewer function.
// This allows us to reuse the existing review infrastructure.
func convertCodeReqToProposal(req *CodeReviewRequest) *proposal.Proposal {
	// Create a minimal proposal that represents the code review
	p := &proposal.Proposal{
		ID:          req.PRID,
		Title:       req.Title,
		Description: req.Description,
		Category:    "code-review",
	}
	return p
}

// extractInlineComments parses evidence strings to extract file/line comments.
// Evidence format expected: "filename.go:123: comment text"
func extractInlineComments(evidence []string, files map[string]string) []pullrequest.InlineComment {
	var comments []pullrequest.InlineComment
	
	for _, ev := range evidence {
		// Try to parse "file:line: message" format
		parts := strings.SplitN(ev, ":", 3)
		if len(parts) < 3 {
			continue
		}
		
		filePath := strings.TrimSpace(parts[0])
		lineNum := 0
		if _, err := fmt.Sscanf(parts[1], "%d", &lineNum); err != nil {
			continue
		}
		message := strings.TrimSpace(parts[2])
		
		// Validate the file exists
		if _, exists := files[filePath]; !exists {
			continue
		}
		
		// Determine severity from message keywords
		severity := "info"
		msgLower := strings.ToLower(message)
		if strings.Contains(msgLower, "vulnerability") || strings.Contains(msgLower, "security") ||
			strings.Contains(msgLower, "injection") || strings.Contains(msgLower, "critical") {
			severity = "error"
		} else if strings.Contains(msgLower, "warning") || strings.Contains(msgLower, "concern") ||
			strings.Contains(msgLower, "should") || strings.Contains(msgLower, "potential") {
			severity = "warning"
		}
		
		comments = append(comments, pullrequest.InlineComment{
			FilePath:   filePath,
			LineNumber: lineNum,
			Comment:    message,
			Severity:   severity,
			Timestamp:  timeNow(),
		})
	}
	
	return comments
}

// Helper functions

func boolToStatus(b bool) string {
	if b {
		return "✅ PASSED"
	}
	return "❌ FAILED"
}

func sortStrings(s []string) {
	// Simple bubble sort for small lists
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

func generateReviewID() string {
	// Generate a unique review ID
	return fmt.Sprintf("review-%d", timeNow().Unix())
}

func timeNow() time.Time {
	return time.Now().UTC()
}
