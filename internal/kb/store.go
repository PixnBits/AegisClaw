// Package kb implements the built-in LLM Knowledge Base (Karpathy-style):
// a self-maintaining, compiled Markdown wiki that grows smarter over time.
//
// Architecture:
//   - Raw source documents are stored in <dir>/raw/ (immutable, append-only)
//   - Compiled wiki pages live in <dir>/wiki/ (derived, always re-generatable)
//   - Every ingest event is signed and logged in the Merkle audit chain
//   - The Compiler and Linter run as built-in scheduled skills
//
// The wiki directory contains standard Markdown files.  Each file's first
// H1 heading is used as the page title.  The index page (index.md) is
// maintained automatically by the Compiler.
package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	// maxIngestSizeBytes is the hard cap for a single ingested document (50 MiB).
	maxIngestSizeBytes = 50 * 1024 * 1024

	// queryExcerptLen is the maximum number of characters shown per result excerpt.
	queryExcerptLen = 300

	// metaSuffix is the sidecar filename suffix for raw-document metadata.
	metaSuffix = ".meta.json"
)

// IngestMeta records provenance for an ingested raw document.
type IngestMeta struct {
	ID           string    `json:"id"`
	OriginalName string    `json:"original_name"`
	ContentType  string    `json:"content_type,omitempty"`
	IngestedAt   time.Time `json:"ingested_at"`
	SHA256       string    `json:"sha256"`
	SizeBytes    int64     `json:"size_bytes"`
	Source       string    `json:"source,omitempty"`
}

// WikiPage describes a compiled wiki page inside <dir>/wiki/.
type WikiPage struct {
	// Slug is the bare filename without the .md extension (e.g. "index").
	Slug string `json:"slug"`
	// Title is extracted from the first H1 heading in the file, or the slug
	// when no H1 is found.
	Title string `json:"title"`
	// UpdatedAt is the file modification time.
	UpdatedAt time.Time `json:"updated_at"`
	// SizeBytes is the raw file size.
	SizeBytes int64 `json:"size_bytes"`
}

// QueryResult is one item returned by Store.Query.
type QueryResult struct {
	Page    WikiPage `json:"page"`
	Excerpt string   `json:"excerpt"`
}

// CompileStats summarises a compiler run.
type CompileStats struct {
	RawExamined int           `json:"raw_examined"`
	PagesWritten int          `json:"pages_written"`
	PagesUpdated int          `json:"pages_updated"`
	ElapsedTime  time.Duration `json:"elapsed_time_ns"`
}

// LintIssue describes a single wiki problem found by the Linter.
type LintIssue struct {
	Page    string `json:"page"`
	Kind    string `json:"kind"` // "orphan", "empty", "no_title"
	Message string `json:"message"`
}

// LintStats summarises a linter run.
type LintStats struct {
	PagesScanned int         `json:"pages_scanned"`
	IssuesFound  int         `json:"issues_found"`
	Issues       []LintIssue `json:"issues,omitempty"`
	ElapsedTime  time.Duration `json:"elapsed_time_ns"`
}

// Store manages the local Knowledge Base directory tree.
type Store struct {
	dir string
	mu  sync.RWMutex
}

// New opens (or creates) a Store rooted at dir.
// The raw/ and wiki/ subdirectories are created if they do not exist.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("kb: dir is required")
	}
	for _, sub := range []string{"raw", "wiki"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			return nil, fmt.Errorf("kb: create %s dir: %w", sub, err)
		}
	}
	return &Store{dir: dir}, nil
}

// Dir returns the base directory of the store.
func (s *Store) Dir() string { return s.dir }

// RawDir returns the path to the raw documents directory.
func (s *Store) RawDir() string { return filepath.Join(s.dir, "raw") }

// WikiDir returns the path to the compiled wiki directory.
func (s *Store) WikiDir() string { return filepath.Join(s.dir, "wiki") }

// Ingest copies the file at srcPath into the raw/ directory and writes a
// JSON metadata sidecar.  Returns the IngestMeta which should be included
// in the audit log payload by the caller.
//
// Security: srcPath is opened read-only; no shell expansion occurs.
// The destination filename is derived from the SHA-256 of the content,
// preventing filename-injection attacks.
func (s *Store) Ingest(srcPath, source string) (*IngestMeta, error) {
	if srcPath == "" {
		return nil, fmt.Errorf("kb ingest: source path is required")
	}

	f, err := os.Open(filepath.Clean(srcPath))
	if err != nil {
		return nil, fmt.Errorf("kb ingest: open %q: %w", srcPath, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("kb ingest: stat %q: %w", srcPath, err)
	}
	if info.Size() > maxIngestSizeBytes {
		return nil, fmt.Errorf("kb ingest: file too large (%d bytes, max %d)", info.Size(), maxIngestSizeBytes)
	}

	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(f, h))
	if err != nil {
		return nil, fmt.Errorf("kb ingest: read %q: %w", srcPath, err)
	}
	digest := hex.EncodeToString(h.Sum(nil))

	origName := filepath.Base(srcPath)
	destName := digest[:12] + "-" + sanitizeFilename(origName)
	destPath := filepath.Join(s.RawDir(), destName)

	s.mu.Lock()
	defer s.mu.Unlock()

	// If the exact content is already present, return existing metadata.
	if existing, ok := s.findByDigestLocked(digest); ok {
		return existing, nil
	}

	if err := os.WriteFile(destPath, data, 0600); err != nil {
		return nil, fmt.Errorf("kb ingest: write raw file: %w", err)
	}

	meta := &IngestMeta{
		ID:           digest[:16],
		OriginalName: origName,
		IngestedAt:   time.Now().UTC(),
		SHA256:       digest,
		SizeBytes:    info.Size(),
		Source:       source,
	}
	metaData, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("kb ingest: marshal metadata: %w", err)
	}
	if err := os.WriteFile(destPath+metaSuffix, metaData, 0600); err != nil {
		_ = os.Remove(destPath)
		return nil, fmt.Errorf("kb ingest: write metadata: %w", err)
	}

	return meta, nil
}

// findByDigestLocked searches raw/ for an existing document with the given
// SHA-256 digest.  Must be called with s.mu held (at least read).
func (s *Store) findByDigestLocked(digest string) (*IngestMeta, bool) {
	entries, err := os.ReadDir(s.RawDir())
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), metaSuffix) {
			continue
		}
		metaPath := filepath.Join(s.RawDir(), e.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var m IngestMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.SHA256 == digest {
			return &m, true
		}
	}
	return nil, false
}

// ListRaw returns metadata for all ingested raw documents, sorted newest-first.
func (s *Store) ListRaw() ([]IngestMeta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.RawDir())
	if err != nil {
		return nil, fmt.Errorf("kb: list raw: %w", err)
	}

	var metas []IngestMeta
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), metaSuffix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.RawDir(), e.Name()))
		if err != nil {
			continue
		}
		var m IngestMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].IngestedAt.After(metas[j].IngestedAt)
	})
	return metas, nil
}

// ListWikiPages returns all compiled wiki pages, sorted by title.
func (s *Store) ListWikiPages() ([]WikiPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.WikiDir())
	if err != nil {
		return nil, fmt.Errorf("kb: list wiki: %w", err)
	}

	var pages []WikiPage
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		info, err := e.Info()
		if err != nil {
			continue
		}
		content, err := os.ReadFile(filepath.Join(s.WikiDir(), e.Name()))
		if err != nil {
			continue
		}
		pages = append(pages, WikiPage{
			Slug:      slug,
			Title:     extractTitle(slug, content),
			UpdatedAt: info.ModTime().UTC(),
			SizeBytes: info.Size(),
		})
	}

	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Slug == "index" {
			return true
		}
		if pages[j].Slug == "index" {
			return false
		}
		return pages[i].Title < pages[j].Title
	})
	return pages, nil
}

// GetWikiPage returns the full Markdown content of a single wiki page by slug.
func (s *Store) GetWikiPage(slug string) (WikiPage, string, error) {
	if slug == "" {
		return WikiPage{}, "", fmt.Errorf("kb: slug is required")
	}
	// Security: prevent directory traversal.
	if strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		return WikiPage{}, "", fmt.Errorf("kb: invalid slug %q", slug)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.WikiDir(), slug+".md")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WikiPage{}, "", fmt.Errorf("kb: wiki page %q not found", slug)
		}
		return WikiPage{}, "", fmt.Errorf("kb: read wiki page %q: %w", slug, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return WikiPage{}, "", fmt.Errorf("kb: stat wiki page %q: %w", slug, err)
	}

	page := WikiPage{
		Slug:      slug,
		Title:     extractTitle(slug, content),
		UpdatedAt: info.ModTime().UTC(),
		SizeBytes: info.Size(),
	}
	return page, string(content), nil
}

// WriteWikiPage writes (or overwrites) a wiki page.  Used by the Compiler.
func (s *Store) WriteWikiPage(slug, content string) error {
	if slug == "" {
		return fmt.Errorf("kb: slug is required")
	}
	if strings.Contains(slug, "/") || strings.Contains(slug, "..") {
		return fmt.Errorf("kb: invalid slug %q", slug)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Join(s.WikiDir(), slug+".md")
	return os.WriteFile(path, []byte(content), 0600)
}

// Query searches the wiki for pages whose content contains any of the
// whitespace-separated terms in the query string (case-insensitive).
// Returns up to limit results (0 = no limit).
func (s *Store) Query(query string, limit int) ([]QueryResult, error) {
	if query == "" {
		return nil, fmt.Errorf("kb query: query string is required")
	}

	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("kb query: no terms found in query")
	}

	pages, err := s.ListWikiPages()
	if err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []QueryResult
	for _, p := range pages {
		if limit > 0 && len(results) >= limit {
			break
		}
		content, err := os.ReadFile(filepath.Join(s.WikiDir(), p.Slug+".md"))
		if err != nil {
			continue
		}
		lower := strings.ToLower(string(content))
		matched := false
		for _, term := range terms {
			if strings.Contains(lower, term) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		results = append(results, QueryResult{
			Page:    p,
			Excerpt: buildExcerpt(string(content), terms[0], queryExcerptLen),
		})
	}
	return results, nil
}

// Compile performs an in-process stub compilation pass: it generates or
// refreshes the wiki index from the list of raw documents and existing pages.
// Production deployments replace this with a full LLM-driven compiler running
// in an ephemeral micro-VM.  This stub ensures the wiki index is always
// up-to-date even without an LLM and provides a signed audit trail.
func (s *Store) Compile() (*CompileStats, error) {
	start := time.Now()
	metas, err := s.ListRaw()
	if err != nil {
		return nil, fmt.Errorf("kb compile: list raw: %w", err)
	}

	// Build the wiki index page from raw document metadata.
	var sb strings.Builder
	sb.WriteString("# Knowledge Base Index\n\n")
	sb.WriteString(fmt.Sprintf("_Last compiled: %s_\n\n", time.Now().UTC().Format(time.RFC3339)))
	if len(metas) == 0 {
		sb.WriteString("No documents ingested yet.  Use `aegisclaw kb ingest <file>` to add source material.\n")
	} else {
		sb.WriteString(fmt.Sprintf("## Source Documents (%d)\n\n", len(metas)))
		sb.WriteString("| Document | Ingested | SHA-256 |\n")
		sb.WriteString("|----------|----------|----------|\n")
		for _, m := range metas {
			sb.WriteString(fmt.Sprintf("| %s | %s | `%s…` |\n",
				m.OriginalName,
				m.IngestedAt.Format("2006-01-02"),
				m.SHA256[:16],
			))
		}
	}

	pages, _ := s.ListWikiPages()
	nonIndex := 0
	for _, p := range pages {
		if p.Slug != "index" {
			nonIndex++
		}
	}
	if nonIndex > 0 {
		sb.WriteString(fmt.Sprintf("\n## Wiki Pages (%d)\n\n", nonIndex))
		for _, p := range pages {
			if p.Slug == "index" {
				continue
			}
			sb.WriteString(fmt.Sprintf("- [%s](%s.md) — _updated %s_\n",
				p.Title, p.Slug, p.UpdatedAt.Format("2006-01-02")))
		}
	}

	written := 0
	updated := 0
	_, _, indexReadErr := s.GetWikiPage("index")
	if err := s.WriteWikiPage("index", sb.String()); err != nil {
		return nil, fmt.Errorf("kb compile: write index: %w", err)
	}
	if indexReadErr != nil {
		written++
	} else {
		updated++
	}

	return &CompileStats{
		RawExamined:  len(metas),
		PagesWritten: written,
		PagesUpdated: updated,
		ElapsedTime:  time.Since(start),
	}, nil
}

// Lint scans the wiki directory for common issues: empty pages, pages with no
// H1 title, and orphaned pages not linked from the index.
func (s *Store) Lint() (*LintStats, error) {
	start := time.Now()
	pages, err := s.ListWikiPages()
	if err != nil {
		return nil, fmt.Errorf("kb lint: list wiki: %w", err)
	}

	// Build set of pages linked from the index.
	linked := make(map[string]bool)
	if _, indexContent, err := s.GetWikiPage("index"); err == nil {
		for _, p := range pages {
			if p.Slug == "index" {
				continue
			}
			if strings.Contains(indexContent, p.Slug+".md") ||
				strings.Contains(indexContent, "("+p.Slug+")") {
				linked[p.Slug] = true
			}
		}
	}

	var issues []LintIssue
	for _, p := range pages {
		if p.Slug == "index" {
			continue
		}
		_, content, err := s.GetWikiPage(p.Slug)
		if err != nil {
			continue
		}
		if strings.TrimSpace(content) == "" {
			issues = append(issues, LintIssue{
				Page:    p.Slug,
				Kind:    "empty",
				Message: fmt.Sprintf("Page %q is empty", p.Slug),
			})
		}
		if p.Title == p.Slug {
			issues = append(issues, LintIssue{
				Page:    p.Slug,
				Kind:    "no_title",
				Message: fmt.Sprintf("Page %q has no H1 title heading", p.Slug),
			})
		}
		if len(pages) > 1 && !linked[p.Slug] {
			issues = append(issues, LintIssue{
				Page:    p.Slug,
				Kind:    "orphan",
				Message: fmt.Sprintf("Page %q is not linked from the index", p.Slug),
			})
		}
	}

	return &LintStats{
		PagesScanned: len(pages),
		IssuesFound:  len(issues),
		Issues:       issues,
		ElapsedTime:  time.Since(start),
	}, nil
}

// Status returns a summary of the knowledge base state.
func (s *Store) Status() (map[string]interface{}, error) {
	metas, err := s.ListRaw()
	if err != nil {
		return nil, err
	}
	pages, err := s.ListWikiPages()
	if err != nil {
		return nil, err
	}
	var lastIngest *time.Time
	if len(metas) > 0 {
		t := metas[0].IngestedAt
		lastIngest = &t
	}
	var lastCompile *time.Time
	for _, p := range pages {
		if p.Slug == "index" {
			t := p.UpdatedAt
			lastCompile = &t
			break
		}
	}
	return map[string]interface{}{
		"raw_documents": len(metas),
		"wiki_pages":    len(pages),
		"last_ingest":   lastIngest,
		"last_compile":  lastCompile,
		"raw_dir":       s.RawDir(),
		"wiki_dir":      s.WikiDir(),
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

// extractTitle returns the text of the first H1 heading in content, or the
// slug if no heading is found.
func extractTitle(slug string, content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(line[2:])
		}
	}
	return slug
}

// buildExcerpt returns a short snippet of text around the first occurrence of
// term in content, truncated to maxLen characters.
func buildExcerpt(content, term string, maxLen int) string {
	lower := strings.ToLower(content)
	idx := strings.Index(lower, strings.ToLower(term))
	if idx < 0 {
		// No match: return the start of the content.
		idx = 0
	}
	start := idx - 80
	if start < 0 {
		start = 0
	}
	// Snap to a rune boundary.
	for start > 0 && !utf8.RuneStart(content[start]) {
		start--
	}
	end := start + maxLen
	if end > len(content) {
		end = len(content)
	}
	for end < len(content) && !utf8.RuneStart(content[end]) {
		end--
	}
	excerpt := content[start:end]
	// Strip Markdown heading markers for cleaner display.
	lines := strings.Split(excerpt, "\n")
	clean := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		l = strings.TrimLeft(l, "#")
		l = strings.TrimSpace(l)
		clean = append(clean, l)
	}
	result := strings.Join(clean, " ")
	if start > 0 {
		result = "…" + result
	}
	if end < len(content) {
		result += "…"
	}
	return result
}

// sanitizeFilename replaces unsafe characters to produce a safe destination
// filename for storing in the raw directory.
func sanitizeFilename(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := b.String()
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}
