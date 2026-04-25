package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/api"
	gitmanager "github.com/PixnBits/AegisClaw/internal/git"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// makeGitBrowseHandler returns files and directories in a git repository.
func makeGitBrowseHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Repo string `json:"repo"` // "skills" or "self"
			Path string `json:"path"` // relative path within repo, defaults to root "/"
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		// Validate repo kind
		var kind gitmanager.RepoKind
		switch req.Repo {
		case "skills":
			kind = gitmanager.RepoSkills
		case "self":
			kind = gitmanager.RepoSelf
		default:
			return &api.Response{Error: "repo must be 'skills' or 'self'"}
		}

		// Get repository path
		var repoPath string
		if kind == gitmanager.RepoSkills {
			repoPath = env.GitManager.SkillsPath()
		} else {
			repoPath = env.GitManager.SelfPath()
		}

		// Normalize and validate path
		requestedPath := filepath.Clean(req.Path)
		if requestedPath == "" || requestedPath == "." {
			requestedPath = "/"
		}
		if !strings.HasPrefix(requestedPath, "/") {
			requestedPath = "/" + requestedPath
		}

		// Prevent path traversal
		if strings.Contains(requestedPath, "..") {
			return &api.Response{Error: "path traversal not allowed"}
		}

		// Build absolute path
		absPath := filepath.Join(repoPath, strings.TrimPrefix(requestedPath, "/"))

		// Check if path exists
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return &api.Response{Error: "path not found"}
			}
			return &api.Response{Error: "failed to stat path: " + err.Error()}
		}

		if info.IsDir() {
			// Return directory listing
			entries, err := os.ReadDir(absPath)
			if err != nil {
				return &api.Response{Error: "failed to read directory: " + err.Error()}
			}

			items := make([]map[string]interface{}, 0, len(entries))
			for _, entry := range entries {
				// Skip .git directory
				if entry.Name() == ".git" {
					continue
				}

				item := map[string]interface{}{
					"name":  entry.Name(),
					"is_dir": entry.IsDir(),
				}

				info, err := entry.Info()
				if err == nil {
					item["size"] = info.Size()
					item["mod_time"] = info.ModTime().Unix()
				}

				items = append(items, item)
			}

			respData, _ := json.Marshal(map[string]interface{}{
				"type":    "directory",
				"path":    requestedPath,
				"items":   items,
			})
			return &api.Response{Success: true, Data: respData}
		}

		// Return file content
		content, err := os.ReadFile(absPath)
		if err != nil {
			return &api.Response{Error: "failed to read file: " + err.Error()}
		}

		respData, _ := json.Marshal(map[string]interface{}{
			"type":    "file",
			"path":    requestedPath,
			"content": string(content),
			"size":    len(content),
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeGitListBranchesHandler lists all branches in a repository.
func makeGitListBranchesHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Repo string `json:"repo"` // "skills" or "self"
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		var kind gitmanager.RepoKind
		switch req.Repo {
		case "skills":
			kind = gitmanager.RepoSkills
		case "self":
			kind = gitmanager.RepoSelf
		default:
			return &api.Response{Error: "repo must be 'skills' or 'self'"}
		}

		branches, err := env.GitManager.ListBranches(kind)
		if err != nil {
			return &api.Response{Error: "failed to list branches: " + err.Error()}
		}

		currentBranch, _ := env.GitManager.GetCurrentBranch(kind)

		respData, _ := json.Marshal(map[string]interface{}{
			"branches":       branches,
			"current_branch": currentBranch,
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeGitCommitHistoryHandler returns commit history for a branch.
func makeGitCommitHistoryHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Repo       string `json:"repo"`        // "skills" or "self"
			ProposalID string `json:"proposal_id"` // optional, if set gets commits for proposal branch
			Limit      int    `json:"limit"`       // max commits to return
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		if req.Limit <= 0 {
			req.Limit = 50
		}

		var kind gitmanager.RepoKind
		switch req.Repo {
		case "skills":
			kind = gitmanager.RepoSkills
		case "self":
			kind = gitmanager.RepoSelf
		default:
			return &api.Response{Error: "repo must be 'skills' or 'self'"}
		}

		if req.ProposalID != "" {
			// Get commits for proposal branch
			commits, err := env.GitManager.GetBranchCommits(kind, req.ProposalID)
			if err != nil {
				return &api.Response{Error: "failed to get branch commits: " + err.Error()}
			}

			// Limit results
			if len(commits) > req.Limit {
				commits = commits[:req.Limit]
			}

			respData, _ := json.Marshal(map[string]interface{}{
				"commits":     commits,
				"proposal_id": req.ProposalID,
			})
			return &api.Response{Success: true, Data: respData}
		}

		// For now, return empty if no proposal ID
		// TODO: implement general commit history browsing
		respData, _ := json.Marshal(map[string]interface{}{
			"commits": []interface{}{},
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeGitDiffHandler generates a diff for a proposal branch.
func makeGitDiffHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Repo       string `json:"repo"`        // "skills" or "self"
			ProposalID string `json:"proposal_id"` // required
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		if req.ProposalID == "" {
			return &api.Response{Error: "proposal_id is required"}
		}

		var kind gitmanager.RepoKind
		switch req.Repo {
		case "skills":
			kind = gitmanager.RepoSkills
		case "self":
			kind = gitmanager.RepoSelf
		default:
			return &api.Response{Error: "repo must be 'skills' or 'self'"}
		}

		diff, err := env.GitManager.GenerateDiff(kind, req.ProposalID)
		if err != nil {
			return &api.Response{Error: "failed to generate diff: " + err.Error()}
		}

		respData, _ := json.Marshal(map[string]interface{}{
			"proposal_id": req.ProposalID,
			"diff":        diff,
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeWorkspaceReadHandler reads workspace files (SOUL.md, AGENTS.md, etc.)
func makeWorkspaceReadHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Filename string `json:"filename"` // e.g., "SOUL.md", "AGENTS.md", "TOOLS.md"
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		// Validate filename - must be one of the allowed workspace files
		allowed := map[string]bool{
			"SOUL.md":   true,
			"AGENTS.md": true,
			"TOOLS.md":  true,
		}

		// Also allow *.SKILL.md files
		isSKILL := strings.HasSuffix(req.Filename, ".SKILL.md")
		
		if !allowed[req.Filename] && !isSKILL {
			return &api.Response{Error: "only workspace files (SOUL.md, AGENTS.md, TOOLS.md, *.SKILL.md) can be accessed"}
		}

		// Prevent path traversal
		if strings.Contains(req.Filename, "/") || strings.Contains(req.Filename, "..") {
			return &api.Response{Error: "invalid filename"}
		}

		workspacePath := filepath.Join(env.Config.Workspace.Dir, req.Filename)

		content, err := os.ReadFile(workspacePath)
		if err != nil {
			if os.IsNotExist(err) {
				// Return empty content if file doesn't exist yet
				respData, _ := json.Marshal(map[string]interface{}{
					"filename": req.Filename,
					"content":  "",
					"exists":   false,
				})
				return &api.Response{Success: true, Data: respData}
			}
			return &api.Response{Error: "failed to read file: " + err.Error()}
		}

		respData, _ := json.Marshal(map[string]interface{}{
			"filename": req.Filename,
			"content":  string(content),
			"exists":   true,
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeWorkspaceWriteHandler writes workspace files with audit logging.
func makeWorkspaceWriteHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		var req struct {
			Filename string `json:"filename"` // e.g., "SOUL.md", "AGENTS.md"
			Content  string `json:"content"`
		}
		if err := json.Unmarshal(data, &req); err != nil {
			return &api.Response{Error: "invalid request: " + err.Error()}
		}

		// Validate filename
		allowed := map[string]bool{
			"SOUL.md":   true,
			"AGENTS.md": true,
			"TOOLS.md":  true,
		}
		isSKILL := strings.HasSuffix(req.Filename, ".SKILL.md")

		if !allowed[req.Filename] && !isSKILL {
			return &api.Response{Error: "only workspace files can be edited"}
		}

		// Prevent path traversal
		if strings.Contains(req.Filename, "/") || strings.Contains(req.Filename, "..") {
			return &api.Response{Error: "invalid filename"}
		}

		workspacePath := filepath.Join(env.Config.Workspace.Dir, req.Filename)

		// Read old content for audit log
		oldContent, _ := os.ReadFile(workspacePath)

		// Write new content
		if err := os.WriteFile(workspacePath, []byte(req.Content), 0644); err != nil {
			return &api.Response{Error: "failed to write file: " + err.Error()}
		}

		// Audit log the change
		auditPayload, _ := json.Marshal(map[string]interface{}{
			"filename":  req.Filename,
			"old_size":  len(oldContent),
			"new_size":  len(req.Content),
			"operation": "workspace.edit",
		})
		action := kernel.NewAction("workspace.edit", "dashboard", auditPayload)
		env.Kernel.SignAndLog(action)

		env.Logger.Info("workspace file updated",
			zap.String("filename", req.Filename),
			zap.Int("old_size", len(oldContent)),
			zap.Int("new_size", len(req.Content)),
		)

		respData, _ := json.Marshal(map[string]interface{}{
			"filename": req.Filename,
			"success":  true,
		})
		return &api.Response{Success: true, Data: respData}
	}
}

// makeWorkspaceListHandler lists all files in the workspace.
func makeWorkspaceListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, data json.RawMessage) *api.Response {
		workspacePath := env.Config.Workspace.Dir

		var files []map[string]interface{}

		err := filepath.WalkDir(workspacePath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			// Skip directories
			if d.IsDir() {
				return nil
			}

			// Get relative path
			relPath, err := filepath.Rel(workspacePath, path)
			if err != nil {
				return err
			}

			// Get file info
			info, err := d.Info()
			if err != nil {
				return nil // Skip files we can't stat
			}

			files = append(files, map[string]interface{}{
				"name":     relPath,
				"size":     info.Size(),
				"mod_time": info.ModTime().Unix(),
			})

			return nil
		})

		if err != nil {
			return &api.Response{Error: "failed to list workspace: " + err.Error()}
		}

		respData, _ := json.Marshal(map[string]interface{}{
			"files": files,
		})
		return &api.Response{Success: true, Data: respData}
	}
}
