package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PixnBits/AegisClaw/internal/llm"
	"github.com/spf13/cobra"
)

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Manage Ollama models",
	Long:  `Commands for listing, verifying, and updating Ollama models used by AegisClaw.`,
}

var modelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all known and available models",
	Long:  `Shows the status of all registered and locally available Ollama models.`,
	RunE:  runModelList,
}

var modelVerifyCmd = &cobra.Command{
	Use:   "verify <model>",
	Short: "Verify a model's integrity",
	Long:  `Checks a model's SHA256 digest against its registered hash in the model registry.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runModelVerify,
}

var modelUpdateCmd = &cobra.Command{
	Use:   "update <model>",
	Short: "Pull and register a model",
	Long:  `Downloads the latest version of a model from Ollama and updates the registry with its digest.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runModelUpdate,
}

func runModelList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	client := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
		Timeout:  time.Duration(env.Config.Ollama.TimeoutSecs) * time.Second,
	})

	registry, err := llm.NewModelRegistry(env.Config.Ollama.RegistryPath)
	if err != nil {
		return fmt.Errorf("load model registry: %w", err)
	}

	mgr := llm.NewManager(client, registry, llm.ManagerConfig{
		ModelDir: env.Config.Ollama.ModelDir,
	}, env.Logger)

	mgr.SyncKnownGood()

	ctx := context.Background()
	statuses, err := mgr.ListStatus(ctx)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not reach Ollama (%v)\nShowing registry-only info.\n\n", err)
		// Fall back to registry-only listing
		for _, entry := range registry.List() {
			hash := entry.SHA256
			if hash == "" {
				hash = "(not set)"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %-25s  hash=%-16s  tags=%s\n",
				entry.Name, truncate(hash, 16), strings.Join(entry.Tags, ","))
		}
		return nil
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Model Status:")
	fmt.Fprintf(cmd.OutOrStdout(), "  %-25s  %-10s  %-10s  %-10s  %-18s  %s\n",
		"NAME", "REGISTERED", "AVAILABLE", "VERIFIED", "DIGEST", "TAGS")
	fmt.Fprintln(cmd.OutOrStdout(), strings.Repeat("-", 100))

	for _, s := range statuses {
		digest := s.Digest
		if digest == "" {
			digest = "-"
		}
		tags := "-"
		if len(s.Tags) > 0 {
			tags = strings.Join(s.Tags, ",")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  %-25s  %-10s  %-10s  %-10s  %-18s  %s\n",
			s.Name, boolYN(s.Registered), boolYN(s.Available), boolYN(s.Verified),
			truncate(digest, 18), tags)
	}
	return nil
}

func runModelVerify(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	client := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
		Timeout:  time.Duration(env.Config.Ollama.TimeoutSecs) * time.Second,
	})

	registry, err := llm.NewModelRegistry(env.Config.Ollama.RegistryPath)
	if err != nil {
		return fmt.Errorf("load model registry: %w", err)
	}

	mgr := llm.NewManager(client, registry, llm.ManagerConfig{
		ModelDir: env.Config.Ollama.ModelDir,
	}, env.Logger)

	ctx := context.Background()
	status, err := mgr.Verify(ctx, args[0])
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Model: %s\n", status.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Registered: %s\n", boolYN(status.Registered))
	fmt.Fprintf(cmd.OutOrStdout(), "  Available:  %s\n", boolYN(status.Available))
	fmt.Fprintf(cmd.OutOrStdout(), "  Digest:     %s\n", status.Digest)
	if status.ExpectedHash != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  Expected:   %s\n", status.ExpectedHash)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  Verified:   %s\n", boolYN(status.Verified))
	if status.Details != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Family:     %s\n", status.Details.Family)
		fmt.Fprintf(cmd.OutOrStdout(), "  Parameters: %s\n", status.Details.ParameterSize)
		fmt.Fprintf(cmd.OutOrStdout(), "  Quant:      %s\n", status.Details.QuantizationLevel)
	}

	if status.Registered && status.Available && !status.Verified && status.ExpectedHash != "" {
		return fmt.Errorf("VERIFICATION FAILED: digest mismatch for %s", status.Name)
	}
	return nil
}

func runModelUpdate(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	client := llm.NewClient(llm.ClientConfig{
		Endpoint: env.Config.Ollama.Endpoint,
		Timeout:  time.Duration(env.Config.Ollama.TimeoutSecs) * time.Second,
	})

	registry, err := llm.NewModelRegistry(env.Config.Ollama.RegistryPath)
	if err != nil {
		return fmt.Errorf("load model registry: %w", err)
	}

	mgr := llm.NewManager(client, registry, llm.ManagerConfig{
		ModelDir: env.Config.Ollama.ModelDir,
	}, env.Logger)

	ctx := context.Background()
	status, err := mgr.Update(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Model %s updated successfully.\n", status.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "  Digest: %s\n", status.Digest)
	fmt.Fprintf(cmd.OutOrStdout(), "  Tags:   %s\n", strings.Join(status.Tags, ", "))
	return nil
}

func boolYN(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-2] + ".."
}
