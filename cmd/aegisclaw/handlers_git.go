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

// makeGitBrowseHandler is stubbed — git operations have moved out of the Host Daemon TCB.
func makeGitBrowseHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "git operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}
// makeGitListBranchesHandler is stubbed — git operations have moved out of the Host Daemon TCB.
func makeGitListBranchesHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "git operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}

// makeGitCommitHistoryHandler is stubbed — git operations have moved out of the Host Daemon TCB.
func makeGitCommitHistoryHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "git operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}
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

// makeGitDiffHandler is stubbed — git operations have moved out of the Host Daemon TCB.
func makeGitDiffHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "git operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}
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

// makeWorkspaceReadHandler is stubbed — workspace operations have moved out of the Host Daemon TCB.
func makeWorkspaceReadHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "workspace operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
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

// makeWorkspaceWriteHandler is stubbed — workspace operations have moved out of the Host Daemon TCB.
func makeWorkspaceWriteHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "workspace operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
}
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

// makeWorkspaceListHandler is stubbed — workspace operations have moved out of the Host Daemon TCB.
func makeWorkspaceListHandler(env *runtimeEnv) api.Handler {
	return func(ctx context.Context, _ json.RawMessage) *api.Response {
		return &api.Response{Error: "workspace operations have moved out of the Host Daemon TCB (see AegisHub + Store VM)"}
	}
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
