// Package workspace provides safe loading of user workspace customization files
// under ~/.aegis/ (AGENTS.md, SOUL.md, TOOLS.md, SKILL.md, and the structured
// agents/ layout per agent-customization.md).
//
// This is Task 7.4. All loading is security-hardened:
// - Paths are strictly sanitized and must resolve under the user's ~/.aegis
// - Size limits per file
// - Permission checks (no world-writable for sensitive content)
// - Basic content validation (reject obvious executable/shell content)
// - No execution or template processing of loaded files
//
// Precedence (highest wins):
//   1. Root flat files (~/.aegis/AGENTS.md etc.)
//   2. Per-agent override (~/.aegis/agents/<name>/...)
//   3. shared/ (~/.aegis/agents/shared/...)
//   4. Built-in defaults (empty / minimal)

package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxFileSize = 128 * 1024 // 128 KiB per customization file
)

var (
	ErrPathTraversal     = errors.New("workspace: path traversal or escape detected")
	ErrFileTooLarge      = errors.New("workspace: file exceeds size limit")
	ErrUnsafePermissions = errors.New("workspace: unsafe file permissions")
	ErrUnsafeContent     = errors.New("workspace: file contains potentially executable or dangerous content")
)

// Context holds the loaded customization data with clear provenance.
type Context struct {
	AGENTS string // Custom agent personas / instructions
	SOUL   string // Core values / system prompt
	TOOLS  string // Tool descriptions
	// Skills is intentionally minimal here; full skill loading can layer on top
	// (see 7.3 discovery). We just surface raw SKILL.md content if present.
	SKILLS string

	// Provenance for debugging / audit
	LoadedFrom map[string]string // e.g. "AGENTS" -> "/home/user/.aegis/AGENTS.md"
}

// Load reads and validates customization files from baseDir (typically ~/.aegis).
// It supports both flat root files and the structured ~/.aegis/agents/ layout.
func Load(baseDir string) (*Context, error) {
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("workspace: cannot determine home directory: %w", err)
		}
		baseDir = filepath.Join(home, ".aegis")
	}

	// Ensure we never escape the intended root
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}

	ctx := &Context{
		LoadedFrom: make(map[string]string),
	}

	// Helper to safely read a file with full validation
	readValidated := func(relPath, key string) (string, error) {
		full := filepath.Join(absBase, relPath)
		absFull, err := filepath.Abs(full)
		if err != nil {
			return "", err
		}
		if !strings.HasPrefix(absFull, absBase) {
			return "", ErrPathTraversal
		}

		info, err := os.Stat(absFull)
		if os.IsNotExist(err) {
			return "", nil // optional file
		}
		if err != nil {
			return "", err
		}

		if info.Size() > maxFileSize {
			return "", fmt.Errorf("%w: %s (%d bytes)", ErrFileTooLarge, relPath, info.Size())
		}

		// Permission check: reject world-writable (and group-writable for sensitive root files)
		mode := info.Mode().Perm()
		if mode&0002 != 0 {
			return "", fmt.Errorf("%w: %s (world-writable, mode %o)", ErrUnsafePermissions, relPath, mode)
		}
		if (strings.HasPrefix(relPath, "AGENTS") || strings.HasPrefix(relPath, "SOUL")) && (mode&0020 != 0) {
			return "", fmt.Errorf("%w: %s (group-writable sensitive file, mode %o)", ErrUnsafePermissions, relPath, mode)
		}

		data, err := os.ReadFile(absFull)
		if err != nil {
			return "", err
		}

		content := string(data)

		// Basic dangerous content rejection (shebang, obvious exec)
		lower := strings.ToLower(content)
		if strings.HasPrefix(strings.TrimSpace(lower), "#!") ||
			strings.Contains(lower, "exec ") ||
			strings.Contains(lower, "system(") {
			return "", fmt.Errorf("%w: %s", ErrUnsafeContent, relPath)
		}

		ctx.LoadedFrom[key] = absFull
		return content, nil
	}

	// 1. Root-level flat files (highest precedence for simplicity)
	if agents, err := readValidated("AGENTS.md", "AGENTS"); err == nil {
		ctx.AGENTS = agents
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if soul, err := readValidated("SOUL.md", "SOUL"); err == nil {
		ctx.SOUL = soul
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if tools, err := readValidated("TOOLS.md", "TOOLS"); err == nil {
		ctx.TOOLS = tools
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if skills, err := readValidated("SKILL.md", "SKILLS"); err == nil {
		ctx.SKILLS = skills
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// 2. Structured agents/ layout (shared + per-agent overrides)
	_ = filepath.Join(absBase, "agents") // reserved for future per-agent selection

	// shared/
	if sharedAgents, _ := readValidated(filepath.Join("agents", "shared", "AGENTS.md"), "shared.AGENTS"); sharedAgents != "" && ctx.AGENTS == "" {
		ctx.AGENTS = sharedAgents
	}
	if sharedTools, _ := readValidated(filepath.Join("agents", "shared", "TOOLS.md"), "shared.TOOLS"); sharedTools != "" && ctx.TOOLS == "" {
		ctx.TOOLS = sharedTools
	}

	// default/ (falls back if no per-agent)
	if defSoul, _ := readValidated(filepath.Join("agents", "default", "SOUL.md"), "default.SOUL"); defSoul != "" && ctx.SOUL == "" {
		ctx.SOUL = defSoul
	}
	if defAgents, _ := readValidated(filepath.Join("agents", "default", "AGENTS.md"), "default.AGENTS"); defAgents != "" && ctx.AGENTS == "" {
		ctx.AGENTS = defAgents
	}

	// Note: Per-agent (e.g. "researcher") overrides would be selected at runtime
	// when the agent knows its persona name. For now we load the "default" set.

	return ctx, nil
}

// MustLoad is like Load but panics on error (convenient for startup).
func MustLoad(baseDir string) *Context {
	ctx, err := Load(baseDir)
	if err != nil {
		panic(fmt.Sprintf("workspace: failed to load customizations: %v", err))
	}
	return ctx
}
