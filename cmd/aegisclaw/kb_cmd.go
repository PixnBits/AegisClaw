package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// kbCmd is the top-level `aegisclaw kb` command group.
var kbCmd = &cobra.Command{
	Use:   "kb",
	Short: "Manage the built-in LLM Knowledge Base",
	Long: `Commands for the AegisClaw Knowledge Base — a self-maintaining, compiled
Markdown wiki that grows smarter over time (Karpathy-style).

The Knowledge Base has two layers:
  raw/   — immutable source documents (PDFs, text, code, etc.)
  wiki/  — compiled, high-quality Markdown pages derived from raw/

Built-in scheduled skills keep the wiki up-to-date:
  kb-compiler  runs every 6 hours (configurable) and regenerates wiki pages
  kb-linter    runs daily and flags contradictions, stale info, orphaned pages

Usage:
  kb ingest <file>         Ingest a document into raw/
  kb query  <text>         Search the compiled wiki
  kb status                Show knowledge base status
  kb compile               Manually trigger a compile run
  kb lint                  Manually trigger a lint scan
`,
}

var kbIngestCmd = &cobra.Command{
	Use:   "ingest <file>",
	Short: "Ingest a document into the Knowledge Base raw store",
	Long: `Copies the file into knowledge/raw/ with a SHA-256 derived filename,
writes a JSON metadata sidecar, and logs a signed kb.ingest entry in the
Merkle audit chain.

Supported file types: plain text, Markdown, source code, PDFs (stored as-is).
The Compiler skill processes raw/ documents to generate or update wiki pages.

Example:
  aegisclaw kb ingest ./design-doc.md
  aegisclaw kb ingest ./paper.pdf --source "arXiv:2401.00001"`,
	Args: cobra.ExactArgs(1),
	RunE: runKBIngest,
}

var kbQueryCmd = &cobra.Command{
	Use:   "query <text>",
	Short: "Search the compiled wiki",
	Long: `Performs a keyword search across all compiled Markdown wiki pages and
returns matching pages with an excerpt around the first match.

Example:
  aegisclaw kb query "transformer architecture"
  aegisclaw kb query "rate limiting" --k 5`,
	Args: cobra.MinimumNArgs(1),
	RunE: runKBQuery,
}

var kbStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Knowledge Base status",
	Long:  `Displays counts of raw documents and wiki pages, last ingest time, and last compile time.`,
	RunE:  runKBStatus,
}

var kbCompileCmd = &cobra.Command{
	Use:   "compile",
	Short: "Manually trigger a Knowledge Base compile run",
	Long: `Runs the KB Compiler immediately, refreshing wiki pages from raw documents.
In production, this also runs automatically every 6 hours (configurable via
knowledge_base.compiler_interval_hours in config.yaml).

Each compile run is logged as a signed kb.compile entry in the audit chain.`,
	RunE: runKBCompile,
}

var kbLintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Manually trigger a Knowledge Base lint scan",
	Long: `Runs the KB Linter immediately, scanning wiki pages for:
  - Empty pages
  - Pages missing an H1 title heading
  - Orphaned pages not linked from the wiki index

In production, the Linter runs automatically every 24 hours (configurable via
knowledge_base.linter_interval_hours in config.yaml).

Each lint run is logged as a signed kb.lint entry in the audit chain.`,
	RunE: runKBLint,
}

var (
	kbIngestSource string
	kbQueryK       int
)

func init() {
	kbIngestCmd.Flags().StringVar(&kbIngestSource, "source", "", "Optional source description (e.g. URL, citation)")
	kbQueryCmd.Flags().IntVarP(&kbQueryK, "k", "k", 10, "Maximum number of results to return")
}

func runKBIngest(_ *cobra.Command, args []string) error {
	srcPath := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if env.KBStore == nil {
		return fmt.Errorf("knowledge base not initialised (check knowledge_base.dir in config)")
	}

	meta, err := env.KBStore.Ingest(srcPath, kbIngestSource)
	if err != nil {
		return fmt.Errorf("kb ingest: %w", err)
	}

	// Audit log the ingest event.
	payload, _ := json.Marshal(map[string]interface{}{
		"id":            meta.ID,
		"original_name": meta.OriginalName,
		"sha256":        meta.SHA256,
		"size_bytes":    meta.SizeBytes,
		"source":        meta.Source,
	})
	action := kernel.NewAction(kernel.ActionKBIngest, "operator", payload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit-log kb ingest", zap.Error(logErr))
	}

	if globalJSON {
		data, _ := json.MarshalIndent(meta, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Ingested: %s\n", meta.OriginalName)
	fmt.Printf("  ID:      %s\n", meta.ID)
	fmt.Printf("  SHA-256: %s\n", meta.SHA256)
	fmt.Printf("  Size:    %d bytes\n", meta.SizeBytes)
	fmt.Printf("  Raw dir: %s\n", env.KBStore.RawDir())
	fmt.Printf("\nRun 'aegisclaw kb compile' to refresh the wiki index.\n")
	return nil
}

func runKBQuery(_ *cobra.Command, args []string) error {
	query := strings.Join(args, " ")

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if env.KBStore == nil {
		return fmt.Errorf("knowledge base not initialised (check knowledge_base.dir in config)")
	}

	results, err := env.KBStore.Query(query, kbQueryK)
	if err != nil {
		return fmt.Errorf("kb query: %w", err)
	}

	// Audit-log the query (best-effort).
	payload, _ := json.Marshal(map[string]interface{}{
		"query":        query,
		"result_count": len(results),
	})
	action := kernel.NewAction(kernel.ActionKBQuery, "operator", payload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit-log kb query", zap.Error(logErr))
	}

	if globalJSON {
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	if len(results) == 0 {
		fmt.Printf("No wiki pages match %q.\n", query)
		fmt.Println("Try 'aegisclaw kb compile' if documents were recently ingested.")
		return nil
	}

	fmt.Printf("Found %d result(s) for %q:\n\n", len(results), query)
	for i, r := range results {
		fmt.Printf("%d. %s  (%s)\n", i+1, r.Page.Title, r.Page.Slug+".md")
		fmt.Printf("   %s\n\n", r.Excerpt)
	}
	return nil
}

func runKBStatus(_ *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if env.KBStore == nil {
		return fmt.Errorf("knowledge base not initialised (check knowledge_base.dir in config)")
	}

	status, err := env.KBStore.Status()
	if err != nil {
		return fmt.Errorf("kb status: %w", err)
	}

	if globalJSON {
		data, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Knowledge Base Status\n")
	fmt.Printf("  Raw documents: %d\n", status["raw_documents"])
	fmt.Printf("  Wiki pages:    %d\n", status["wiki_pages"])
	fmt.Printf("  Raw dir:       %s\n", status["raw_dir"])
	fmt.Printf("  Wiki dir:      %s\n", status["wiki_dir"])

	if t, ok := status["last_ingest"].(*time.Time); ok && t != nil {
		fmt.Printf("  Last ingest:   %s\n", t.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Printf("  Last ingest:   never\n")
	}
	if t, ok := status["last_compile"].(*time.Time); ok && t != nil {
		fmt.Printf("  Last compile:  %s\n", t.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Printf("  Last compile:  never\n")
	}

	fmt.Printf("\nCompiler: kb-compiler skill (every %d hours)\n", env.Config.KnowledgeBase.CompilerIntervalHours)
	fmt.Printf("Linter:   kb-linter skill (every %d hours)\n", env.Config.KnowledgeBase.LinterIntervalHours)
	fmt.Printf("Model:    %s\n", env.Config.KnowledgeBase.CompilerModel)
	return nil
}

func runKBCompile(_ *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if env.KBStore == nil {
		return fmt.Errorf("knowledge base not initialised")
	}

	fmt.Printf("Running KB Compiler...\n")
	stats, err := env.KBStore.Compile()
	if err != nil {
		return fmt.Errorf("kb compile: %w", err)
	}

	// Audit log.
	payload, _ := json.Marshal(map[string]interface{}{
		"raw_examined":  stats.RawExamined,
		"pages_written": stats.PagesWritten,
		"pages_updated": stats.PagesUpdated,
		"source":        "manual",
	})
	action := kernel.NewAction(kernel.ActionKBCompile, "operator", payload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit-log kb compile", zap.Error(logErr))
	}

	if globalJSON {
		data, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Compile complete.\n")
	fmt.Printf("  Raw examined:   %d\n", stats.RawExamined)
	fmt.Printf("  Pages written:  %d\n", stats.PagesWritten)
	fmt.Printf("  Pages updated:  %d\n", stats.PagesUpdated)
	fmt.Printf("  Elapsed:        %s\n", stats.ElapsedTime.Round(time.Millisecond))
	return nil
}

func runKBLint(_ *cobra.Command, _ []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	if env.KBStore == nil {
		return fmt.Errorf("knowledge base not initialised")
	}

	fmt.Printf("Running KB Linter...\n")
	stats, err := env.KBStore.Lint()
	if err != nil {
		return fmt.Errorf("kb lint: %w", err)
	}

	// Audit log.
	payload, _ := json.Marshal(map[string]interface{}{
		"pages_scanned": stats.PagesScanned,
		"issues_found":  stats.IssuesFound,
		"source":        "manual",
	})
	action := kernel.NewAction(kernel.ActionKBLint, "operator", payload)
	if _, logErr := env.Kernel.SignAndLog(action); logErr != nil {
		env.Logger.Error("failed to audit-log kb lint", zap.Error(logErr))
	}

	if globalJSON {
		data, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	fmt.Printf("Lint complete.\n")
	fmt.Printf("  Pages scanned: %d\n", stats.PagesScanned)
	fmt.Printf("  Issues found:  %d\n", stats.IssuesFound)
	if len(stats.Issues) > 0 {
		fmt.Println()
		for _, issue := range stats.Issues {
			fmt.Printf("  [%s] %s: %s\n", issue.Kind, issue.Page, issue.Message)
		}
	} else {
		fmt.Println("  No issues found. Wiki is clean.")
	}
	return nil
}
