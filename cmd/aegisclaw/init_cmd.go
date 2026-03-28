package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/kernel"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var initProfile string
var initStrictness string

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize AegisClaw (first-time setup)",
	Long: `One-time setup: creates the ~/.config/aegisclaw/ and
~/.local/share/aegisclaw/ directory structures, loads default configuration,
generates an Ed25519 keypair, and opens the Merkle-tree audit log.

Use --profile to select a user profile which determines the default
strictness level. Use --strictness to override.`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initProfile, "profile", "hobbyist", "User profile: hobbyist, startup, enterprise")
	initCmd.Flags().StringVar(&initStrictness, "strictness", "", "Strictness level: high, medium, low (default: derived from profile)")
}

func runInit(cmd *cobra.Command, args []string) error {
	logger, err := zap.NewProduction()
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	defer logger.Sync()

	// Derive strictness from profile if not set.
	if initStrictness == "" {
		switch initProfile {
		case "enterprise":
			initStrictness = "high"
		case "startup":
			initStrictness = "medium"
		default:
			initStrictness = "low"
		}
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to determine home directory: %w", err)
	}

	aegisDir := filepath.Join(homeDir, ".aegisclaw")

	fmt.Println("Initializing AegisClaw...")
	fmt.Printf("  Profile:    %s\n", initProfile)
	fmt.Printf("  Strictness: %s\n", initStrictness)
	fmt.Printf("  Directory:  %s\n", aegisDir)

	// Create directory structure.
	dirs := []string{
		filepath.Join(homeDir, ".config", "aegisclaw"),
		filepath.Join(homeDir, ".config", "aegisclaw", "personas"),
		filepath.Join(homeDir, ".config", "aegisclaw", "secrets"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw", "audit"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw", "proposals"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw", "sandboxes"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw", "registry"),
		filepath.Join(homeDir, ".local", "share", "aegisclaw", "builder"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("failed to create %s: %w", d, err)
		}
	}

	// Load config (creates defaults if needed).
	cfg, err := config.Load(logger)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize kernel (creates keypair and audit log).
	kern, err := kernel.GetInstance(logger, cfg.Audit.Dir)
	if err != nil {
		return fmt.Errorf("failed to initialize kernel: %w", err)
	}

	// Log init action.
	payload := fmt.Appendf(nil, `{"profile":%q,"strictness":%q}`, initProfile, initStrictness)
	action := kernel.NewAction(kernel.ActionKernelStart, "init", payload)
	if _, logErr := kern.SignAndLog(action); logErr != nil {
		logger.Error("failed to audit log init", zap.Error(logErr))
	}

	kern.Shutdown()

	fmt.Println()
	fmt.Println("AegisClaw initialized successfully.")
	fmt.Printf("  Public Key: %x\n", kern.PublicKey())
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  aegisclaw start       # Start the coordinator daemon")
	fmt.Println("  aegisclaw chat        # Enter interactive chat")

	return nil
}
