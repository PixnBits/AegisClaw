package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTemp writes a temporary file with the given content and returns its path.
func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeTemp %q: %v", name, err)
	}
	return path
}

func TestNew_CreatesDirectories(t *testing.T) {
	base := t.TempDir()
	s, err := New(base)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(s.RawDir()); err != nil {
		t.Errorf("raw dir not created: %v", err)
	}
	if _, err := os.Stat(s.WikiDir()); err != nil {
		t.Errorf("wiki dir not created: %v", err)
	}
}

func TestNew_EmptyDir(t *testing.T) {
	_, err := New("")
	if err == nil {
		t.Fatal("expected error for empty dir")
	}
}

func TestIngest_BasicRoundtrip(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)

	src := writeTemp(t, t.TempDir(), "test.txt", "hello knowledge base")
	meta, err := s.Ingest(src, "unit-test")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if meta.OriginalName != "test.txt" {
		t.Errorf("OriginalName: got %q, want %q", meta.OriginalName, "test.txt")
	}
	if meta.SHA256 == "" {
		t.Error("SHA256 must not be empty")
	}
	if meta.SizeBytes != int64(len("hello knowledge base")) {
		t.Errorf("SizeBytes: got %d, want %d", meta.SizeBytes, len("hello knowledge base"))
	}
	if meta.Source != "unit-test" {
		t.Errorf("Source: got %q, want %q", meta.Source, "unit-test")
	}
	if meta.IngestedAt.IsZero() {
		t.Error("IngestedAt must not be zero")
	}

	// Verify the file was written to raw/.
	entries, _ := os.ReadDir(s.RawDir())
	docCount := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), metaSuffix) {
			docCount++
		}
	}
	if docCount != 1 {
		t.Errorf("expected 1 raw document, got %d", docCount)
	}
}

func TestIngest_DuplicateContentIsIdempotent(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)

	srcDir := t.TempDir()
	src1 := writeTemp(t, srcDir, "doc1.txt", "same content")
	src2 := writeTemp(t, srcDir, "doc2.txt", "same content")

	m1, err := s.Ingest(src1, "")
	if err != nil {
		t.Fatalf("Ingest 1: %v", err)
	}
	m2, err := s.Ingest(src2, "")
	if err != nil {
		t.Fatalf("Ingest 2: %v", err)
	}
	if m1.SHA256 != m2.SHA256 {
		t.Errorf("expected same SHA256 for duplicate content")
	}

	// Only one document file should exist.
	entries, _ := os.ReadDir(s.RawDir())
	docCount := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), metaSuffix) {
			docCount++
		}
	}
	if docCount != 1 {
		t.Errorf("expected 1 raw document after dedup, got %d", docCount)
	}
}

func TestIngest_FileNotFound(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)
	_, err := s.Ingest("/nonexistent/path/nope.txt", "")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestListRaw_Empty(t *testing.T) {
	s, _ := New(t.TempDir())
	metas, err := s.ListRaw()
	if err != nil {
		t.Fatalf("ListRaw: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("expected 0 metas, got %d", len(metas))
	}
}

func TestListRaw_Order(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)
	srcDir := t.TempDir()

	for i, content := range []string{"alpha", "beta", "gamma"} {
		src := writeTemp(t, srcDir, content+".txt", content)
		_, err := s.Ingest(src, "")
		if err != nil {
			t.Fatalf("Ingest %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond) // ensure ordering
	}

	metas, err := s.ListRaw()
	if err != nil {
		t.Fatalf("ListRaw: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("expected 3 metas, got %d", len(metas))
	}
	// Newest-first.
	if !metas[0].IngestedAt.After(metas[1].IngestedAt) {
		t.Errorf("expected newest-first ordering")
	}
}

func TestWriteAndGetWikiPage(t *testing.T) {
	s, _ := New(t.TempDir())

	content := "# My Page\n\nHello, wiki world.\n"
	if err := s.WriteWikiPage("my-page", content); err != nil {
		t.Fatalf("WriteWikiPage: %v", err)
	}

	page, got, err := s.GetWikiPage("my-page")
	if err != nil {
		t.Fatalf("GetWikiPage: %v", err)
	}
	if got != content {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}
	if page.Title != "My Page" {
		t.Errorf("Title: got %q, want %q", page.Title, "My Page")
	}
	if page.Slug != "my-page" {
		t.Errorf("Slug: got %q, want %q", page.Slug, "my-page")
	}
}

func TestGetWikiPage_NotFound(t *testing.T) {
	s, _ := New(t.TempDir())
	_, _, err := s.GetWikiPage("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing page")
	}
}

func TestGetWikiPage_InvalidSlug(t *testing.T) {
	s, _ := New(t.TempDir())
	_, _, err := s.GetWikiPage("../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path-traversal slug")
	}
	_, _, err = s.GetWikiPage("a/b")
	if err == nil {
		t.Fatal("expected error for slash in slug")
	}
}

func TestListWikiPages(t *testing.T) {
	s, _ := New(t.TempDir())

	pages := map[string]string{
		"index":   "# Index\n\nWelcome.",
		"golang":  "# Go Language\n\nFast compiled language.",
		"systems": "# Systems Design\n\nScalability patterns.",
	}
	for slug, content := range pages {
		if err := s.WriteWikiPage(slug, content); err != nil {
			t.Fatalf("WriteWikiPage %q: %v", slug, err)
		}
	}

	listed, err := s.ListWikiPages()
	if err != nil {
		t.Fatalf("ListWikiPages: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 pages, got %d", len(listed))
	}
	// Index must be first.
	if listed[0].Slug != "index" {
		t.Errorf("expected index first, got %q", listed[0].Slug)
	}
}

func TestQuery_Match(t *testing.T) {
	s, _ := New(t.TempDir())
	_ = s.WriteWikiPage("golang", "# Go Language\n\nGo is a fast compiled language by Google.")
	_ = s.WriteWikiPage("rust", "# Rust Language\n\nRust is a memory-safe systems language.")

	// "Google" only appears in the golang page.
	results, err := s.Query("Google", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Page.Slug != "golang" {
		t.Errorf("expected golang result, got %q", results[0].Page.Slug)
	}
	if results[0].Excerpt == "" {
		t.Error("excerpt must not be empty")
	}
}

func TestQuery_NoMatch(t *testing.T) {
	s, _ := New(t.TempDir())
	_ = s.WriteWikiPage("golang", "# Go Language\n\nGo is a fast compiled language.")

	results, err := s.Query("python", 10)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestQuery_EmptyQuery(t *testing.T) {
	s, _ := New(t.TempDir())
	_, err := s.Query("", 10)
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestQuery_Limit(t *testing.T) {
	s, _ := New(t.TempDir())
	for i := 0; i < 5; i++ {
		slug := strings.Repeat(string(rune('a'+i)), 4)
		_ = s.WriteWikiPage(slug, "# Page\n\ncommon term here")
	}

	results, err := s.Query("common term", 3)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestCompile_CreatesIndex(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)

	// Ingest a document first.
	src := writeTemp(t, t.TempDir(), "notes.txt", "Important system design notes.")
	_, err := s.Ingest(src, "test")
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	stats, err := s.Compile()
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if stats.RawExamined != 1 {
		t.Errorf("RawExamined: got %d, want 1", stats.RawExamined)
	}

	// Verify the index page was created.
	_, content, err := s.GetWikiPage("index")
	if err != nil {
		t.Fatalf("GetWikiPage index: %v", err)
	}
	if !strings.Contains(content, "notes.txt") {
		t.Errorf("index should mention ingested document name")
	}
	if !strings.Contains(content, "Knowledge Base Index") {
		t.Errorf("index should have H1 title")
	}
}

func TestLint_DetectsIssues(t *testing.T) {
	s, _ := New(t.TempDir())

	// Page with no H1 title (slug will be used as title).
	_ = s.WriteWikiPage("untitled", "some content without a heading")
	// Empty page.
	_ = s.WriteWikiPage("empty-page", "   ")
	// Index page (excluded from orphan checks).
	_ = s.WriteWikiPage("index", "# Index\n\nWelcome.")

	stats, err := s.Lint()
	if err != nil {
		t.Fatalf("Lint: %v", err)
	}
	if stats.PagesScanned < 3 {
		t.Errorf("PagesScanned: got %d, want ≥3", stats.PagesScanned)
	}

	kinds := make(map[string]bool)
	for _, issue := range stats.Issues {
		kinds[issue.Kind] = true
	}
	if !kinds["empty"] {
		t.Error("expected 'empty' lint issue")
	}
	if !kinds["no_title"] {
		t.Error("expected 'no_title' lint issue")
	}
}

func TestStatus(t *testing.T) {
	base := t.TempDir()
	s, _ := New(base)

	status, err := s.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status["raw_documents"].(int) != 0 {
		t.Errorf("expected 0 raw documents")
	}
	if status["wiki_pages"].(int) != 0 {
		t.Errorf("expected 0 wiki pages")
	}

	// Ingest and compile, then verify counts update.
	src := writeTemp(t, t.TempDir(), "readme.md", "# README\n\nHello.")
	_, _ = s.Ingest(src, "test")
	_, _ = s.Compile()

	status, _ = s.Status()
	if status["raw_documents"].(int) != 1 {
		t.Errorf("expected 1 raw document after ingest")
	}
	if status["wiki_pages"].(int) != 1 {
		t.Errorf("expected 1 wiki page after compile")
	}
}

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		slug    string
		content string
		want    string
	}{
		{"my-slug", "# Hello World\n\nBody.", "Hello World"},
		{"my-slug", "no heading here", "my-slug"},
		{"my-slug", "  # Indented\n\nbody", "Indented"},
		{"my-slug", "", "my-slug"},
	}
	for _, c := range cases {
		got := extractTitle(c.slug, []byte(c.content))
		if got != c.want {
			t.Errorf("extractTitle(%q, %q) = %q, want %q", c.slug, c.content, got, c.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello.txt", "hello.txt"},
		{"my file (1).pdf", "my_file__1_.pdf"},
		{"../../etc/passwd", ".._.._etc_passwd"},
		{strings.Repeat("a", 100), strings.Repeat("a", 64)},
	}
	for _, c := range cases {
		got := sanitizeFilename(c.input)
		if got != c.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
