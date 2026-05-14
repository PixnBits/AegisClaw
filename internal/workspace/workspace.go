package workspace

import (
"fmt"
"os"
"path/filepath"
"strings"
)

// maxFileBytes is the maximum size for any single workspace file.
const maxFileBytes = 256 * 1024 // 256 KiB

// Content holds the parsed content of a workspace directory.
// Each field corresponds to a well-known file in the workspace.
type Content struct {
Path   string
Data   []byte
Soul   string // SOUL.md
Agents string // AGENTS.md
Tools  string // TOOLS.md
Skill  string // SKILL.md
}

// IsEmpty returns true if none of the named workspace files are present.
func (c *Content) IsEmpty() bool {
return c == nil || (c.Soul == "" && c.Agents == "" && c.Tools == "" && c.Skill == "")
}

// fileMap maps file names to the Content field they populate.
var fileMap = []struct {
name   string
setter func(*Content, string)
}{
{"AGENTS.md", func(c *Content, v string) { c.Agents = v }},
{"SOUL.md", func(c *Content, v string) { c.Soul = v }},
{"TOOLS.md", func(c *Content, v string) { c.Tools = v }},
{"SKILL.md", func(c *Content, v string) { c.Skill = v }},
}

// Load reads the well-known workspace files from dir.
// If dir is empty or does not exist, an empty Content is returned without error.
// If dir exists but is not a directory, an error is returned.
// If any file exceeds maxFileBytes, an error is returned.
func Load(dir string) (*Content, error) {
if dir == "" {
return &Content{}, nil
}

info, err := os.Stat(dir)
if os.IsNotExist(err) {
return &Content{}, nil
}
if err != nil {
return nil, err
}
if !info.IsDir() {
return nil, fmt.Errorf("workspace path %q is not a directory", dir)
}

c := &Content{Path: dir}
for _, f := range fileMap {
raw, err := os.ReadFile(filepath.Join(dir, f.name))
if os.IsNotExist(err) {
continue
}
if err != nil {
return nil, fmt.Errorf("reading %s: %w", f.name, err)
}
if len(raw) > maxFileBytes {
return nil, fmt.Errorf("%s exceeds maximum size of %d bytes", f.name, maxFileBytes)
}
f.setter(c, strings.TrimSpace(string(raw)))
}
return c, nil
}

// Workspace holds a reference to a workspace directory.
type Workspace struct {
dir string
}

// New returns a Workspace rooted at the given directory path.
func New(dir string) *Workspace {
return &Workspace{dir: dir}
}

// Load loads the workspace content.
func (w *Workspace) Load() (*Content, error) {
return Load(w.dir)
}

// Edit represents a file edit to apply to a workspace.
type Edit struct {
Path    string
Content string
}
