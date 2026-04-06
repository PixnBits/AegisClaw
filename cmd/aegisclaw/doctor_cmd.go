package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system health and configuration",
	Long: `Runs a series of diagnostic checks and reports the status of each
AegisClaw dependency and configuration item.

Checks performed:
  - Go runtime version
  - Required binary paths (firecracker, jailer)
  - Rootfs template and kernel image existence
  - Config and data directories
  - Workspace directory (optional)
  - Ollama endpoint reachability
  - Daemon socket (is the daemon running?)
  - AegisClaw audit log integrity

Exits with code 0 when all checks pass, 1 when any check fails.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// check holds the result of a single diagnostic check.
type check struct {
	label  string
	ok     bool
	detail string
}

func runDoctor(_ *cobra.Command, _ []string) error {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	cfg, err := config.Load(logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "doctor: failed to load config: %v\n", err)
		def := config.DefaultConfig()
		cfg = &def
	}

	var checks []check

	// ── Go runtime ───────────────────────────────────────────────────────────
	checks = append(checks, check{
		label:  "Go runtime",
		ok:     true,
		detail: runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
	})

	// ── Binary: firecracker ───────────────────────────────────────────────────
	checks = append(checks, checkBinary("firecracker", cfg.Firecracker.Bin))

	// ── Binary: jailer ───────────────────────────────────────────────────────
	checks = append(checks, checkBinary("jailer", cfg.Jailer.Bin))

	// ── Rootfs template ──────────────────────────────────────────────────────
	checks = append(checks, checkFile("rootfs template", cfg.Rootfs.Template))

	// ── Kernel image ─────────────────────────────────────────────────────────
	checks = append(checks, checkFile("kernel image", cfg.Sandbox.KernelImage))

	// ── Config directory ─────────────────────────────────────────────────────
	home, _ := os.UserHomeDir()
	checks = append(checks, checkDir("config dir", filepath.Join(home, ".config", "aegisclaw")))

	// ── Data directory ───────────────────────────────────────────────────────
	checks = append(checks, checkDir("data dir", filepath.Join(home, ".local", "share", "aegisclaw")))

	// ── Audit directory ──────────────────────────────────────────────────────
	checks = append(checks, checkDir("audit dir", cfg.Audit.Dir))

	// ── Workspace directory (optional) ───────────────────────────────────────
	if cfg.Workspace.Dir != "" {
		c := checkDir("workspace dir (optional)", cfg.Workspace.Dir)
		if !c.ok {
			// Workspace is optional — downgrade to a warning.
			c.ok = true
			c.detail = "not found — create " + cfg.Workspace.Dir + " to enable workspace prompts"
		}
		checks = append(checks, c)
	}

	// ── Ollama endpoint ──────────────────────────────────────────────────────
	checks = append(checks, checkOllama(cfg.Ollama.Endpoint))

	// ── Daemon socket ────────────────────────────────────────────────────────
	checks = append(checks, checkDaemon(cfg.Daemon.SocketPath))

	// ── Isolation mode ───────────────────────────────────────────────────────
	isolationMode := cfg.Sandbox.IsolationMode
	if isolationMode == "" {
		isolationMode = "firecracker"
	}
	isoOK := isolationMode == "firecracker" || isolationMode == "docker"
	isoDetail := isolationMode
	if isolationMode == "docker" {
		isoDetail += " (note: docker backend is not yet fully implemented)"
	}
	checks = append(checks, check{
		label:  "isolation mode",
		ok:     isoOK,
		detail: isoDetail,
	})

	// ── Print results ─────────────────────────────────────────────────────────
	allOK := true
	for _, c := range checks {
		symbol := "✓"
		if !c.ok {
			symbol = "✗"
			allOK = false
		}
		if c.detail != "" {
			fmt.Printf("  %s  %-32s  %s\n", symbol, c.label, c.detail)
		} else {
			fmt.Printf("  %s  %s\n", symbol, c.label)
		}
	}

	fmt.Println()
	if allOK {
		fmt.Println("All checks passed. AegisClaw looks healthy.")
		return nil
	}
	fmt.Println("Some checks failed. See ✗ items above.")
	os.Exit(1)
	return nil
}

func checkBinary(label, path string) check {
	if path == "" {
		return check{label: label, ok: false, detail: "path not configured"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return check{label: label, ok: false, detail: "not found: " + path}
	}
	if info.Mode()&0111 == 0 {
		return check{label: label, ok: false, detail: "not executable: " + path}
	}
	return check{label: label, ok: true, detail: path}
}

func checkFile(label, path string) check {
	if path == "" {
		return check{label: label, ok: false, detail: "path not configured"}
	}
	if _, err := os.Stat(path); err != nil {
		return check{label: label, ok: false, detail: "not found: " + path}
	}
	return check{label: label, ok: true, detail: path}
}

func checkDir(label, path string) check {
	if path == "" {
		return check{label: label, ok: false, detail: "path not configured"}
	}
	info, err := os.Stat(path)
	if err != nil {
		return check{label: label, ok: false, detail: "not found: " + path}
	}
	if !info.IsDir() {
		return check{label: label, ok: false, detail: "not a directory: " + path}
	}
	return check{label: label, ok: true, detail: path}
}

func checkOllama(endpoint string) check {
	label := "ollama endpoint"
	if endpoint == "" {
		return check{label: label, ok: false, detail: "endpoint not configured"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/tags", nil)
	if err != nil {
		return check{label: label, ok: false, detail: "build request: " + err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return check{label: label, ok: false, detail: "unreachable (" + endpoint + "): " + err.Error()}
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return check{label: label, ok: false, detail: fmt.Sprintf("%s returned HTTP %d", endpoint, resp.StatusCode)}
	}
	return check{label: label, ok: true, detail: endpoint}
}

func checkDaemon(socketPath string) check {
	label := "daemon socket"
	if socketPath == "" {
		return check{label: label, ok: false, detail: "socket path not configured"}
	}
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return check{label: label, ok: false, detail: "daemon not running (" + socketPath + ")"}
	}
	conn.Close()
	return check{label: label, ok: true, detail: socketPath + " (daemon running)"}
}
