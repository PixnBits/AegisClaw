// Package workspace loads optional prompt-injection files from the user's
// AegisClaw workspace directory (~/.aegisclaw/workspace/ by default).
//
// Inspired by OpenClaw's workspace model, users can place the following files
// in the workspace directory to customise agent behaviour without modifying
// code or going through the full Governance Court SDLC:
//
//   - AGENTS.md  — custom agent persona / identity overrides
//   - SOUL.md    — guiding principles and values for the agent
//   - TOOLS.md   — hints about which tools to prefer or avoid
//   - SKILL.md   — per-skill context injected during skill builds
//
// All files are optional; missing files are silently ignored. File size is
// capped to prevent prompt-stuffing attacks.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// maxFileBytes is the per-file size cap applied when reading workspace files.
// Large files would balloon the LLM context without meaningful benefit and
// could be used to hijack the model via prompt injection.
const maxFileBytes = 16 * 1024 // 16 KiB

// Content holds the parsed contents of the workspace directory.
// Fields are empty strings when the corresponding file is absent.
type Content struct {
	// Agents is the content of AGENTS.md — agent persona/identity overrides.
	Agents string
	// Soul is the content of SOUL.md — guiding principles and values.
	Soul string
	// Tools is the content of TOOLS.md — tool preference hints.
	Tools string
	// Skill is the content of SKILL.md — context injected during skill builds.
	Skill string
}

// IsEmpty returns true when no workspace files were found.
func (c *Content) IsEmpty() bool {
	return c.Agents == "" && c.Soul == "" && c.Tools == "" && c.Skill == ""
}

// Load reads workspace prompt files from dir. Missing files are silently
// skipped; read errors on existing files are returned. The directory itself
// need not exist — Load returns an empty Content in that case.
func Load(dir string) (*Content, error) {
	if dir == "" {
		return &Content{}, nil
	}

	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return &Content{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("workspace: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace: %s is not a directory", dir)
	}

	c := &Content{}
	files := map[string]*string{
		"AGENTS.md": &c.Agents,
		"SOUL.md":   &c.Soul,
		"TOOLS.md":  &c.Tools,
		"SKILL.md":  &c.Skill,
	}

	for name, dest := range files {
		text, err := readCapped(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("workspace: read %s: %w", name, err)
		}
		*dest = text
	}

	return c, nil
}

// readCapped reads the file at path and returns its contents as a trimmed
// string. Returns ("", nil) if the file does not exist. Returns an error if
// the file exceeds maxFileBytes to guard against prompt-stuffing.
func readCapped(path string) (string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > maxFileBytes {
		return "", fmt.Errorf("%s exceeds %d-byte workspace file limit (%d bytes)",
			filepath.Base(path), maxFileBytes, info.Size())
	}

	buf := make([]byte, info.Size())
	n, err := f.Read(buf)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(buf[:n])), nil
}
