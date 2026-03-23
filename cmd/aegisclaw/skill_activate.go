package main

import (
	"encoding/json"
	"fmt"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage AegisClaw skills",
	Long:  "Commands for activating, deactivating, and listing skills.",
}

var skillActivateCmd = &cobra.Command{
	Use:   "activate <name>",
	Short: "Activate a skill in its own microVM",
	Long:  "Spins up a new Firecracker microVM, registers the skill in the persistent registry with a Merkle hash chain.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillActivate,
}

var skillDeactivateCmd = &cobra.Command{
	Use:   "deactivate <name>",
	Short: "Deactivate a running skill",
	Long:  "Stops the skill's microVM and marks it as inactive in the registry.",
	Args:  cobra.ExactArgs(1),
	RunE:  runSkillDeactivate,
}

var skillListCmd = &cobra.Command{
	Use:   "ls",
	Short: "List registered skills",
	RunE:  runSkillList,
}

func runSkillActivate(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	fmt.Printf("Activating skill '%s' via daemon...\n", name)

	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.activate", api.SkillActivateRequest{
		Name: name,
	})
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running? Start it with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("skill activation failed: %s", resp.Error)
	}

	var result struct {
		Name      string `json:"name"`
		SandboxID string `json:"sandbox_id"`
		PID       int    `json:"pid"`
		Version   int    `json:"version"`
		Hash      string `json:"hash"`
		RootHash  string `json:"root_hash"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		fmt.Println("Skill activated (could not parse details).")
		return nil
	}

	fmt.Printf("Skill '%s' activated.\n", result.Name)
	fmt.Printf("  Sandbox: %s (pid=%d)\n", result.SandboxID, result.PID)
	fmt.Printf("  Registry: v%d hash=%s\n", result.Version, result.Hash)
	fmt.Printf("  Root hash: %s\n", result.RootHash)
	return nil
}

func runSkillDeactivate(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	fmt.Printf("Deactivating skill '%s' via daemon...\n", name)

	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.deactivate", api.SkillDeactivateRequest{
		Name: name,
	})
	if err != nil {
		return fmt.Errorf("failed to contact daemon: %w\n(Is the daemon running? Start it with: sudo aegisclaw start)", err)
	}
	if !resp.Success {
		return fmt.Errorf("skill deactivation failed: %s", resp.Error)
	}

	fmt.Printf("Skill '%s' deactivated.\n", name)
	return nil
}

func runSkillList(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	// Try daemon first for live state.
	client := api.NewClient(env.Config.Daemon.SocketPath)
	resp, err := client.Call(cmd.Context(), "skill.list", nil)
	if err == nil && resp.Success {
		var skills []struct {
			Name       string `json:"name"`
			SandboxID  string `json:"sandbox_id"`
			State      string `json:"state"`
			Version    int    `json:"version"`
			MerkleHash string `json:"merkle_hash"`
		}
		if json.Unmarshal(resp.Data, &skills) == nil {
			if len(skills) == 0 {
				fmt.Println("No skills registered.")
				return nil
			}
			fmt.Printf("%-20s %-36s %-10s %-4s %-16s\n",
				"NAME", "SANDBOX", "STATE", "VER", "HASH")
			for _, sk := range skills {
				hashDisplay := sk.MerkleHash
				if len(hashDisplay) > 16 {
					hashDisplay = hashDisplay[:16]
				}
				fmt.Printf("%-20s %-36s %-10s %-4d %-16s\n",
					sk.Name, sk.SandboxID, sk.State, sk.Version, hashDisplay)
			}
			return nil
		}
	}

	// Fall back to local registry.
	skills := env.Registry.List()
	if len(skills) == 0 {
		fmt.Println("No skills registered.")
		return nil
	}

	fmt.Printf("%-20s %-36s %-10s %-4s %-16s\n",
		"NAME", "SANDBOX", "STATE", "VER", "HASH")
	for _, sk := range skills {
		hashDisplay := sk.MerkleHash
		if len(hashDisplay) > 16 {
			hashDisplay = hashDisplay[:16]
		}
		fmt.Printf("%-20s %-36s %-10s %-4d %-16s\n",
			sk.Name, sk.SandboxID, sk.State, sk.Version, hashDisplay)
	}

	rootHash := env.Registry.RootHash()
	if len(rootHash) > 16 {
		rootHash = rootHash[:16]
	}
	fmt.Printf("\nRegistry: seq=%d root=%s\n", env.Registry.Sequence(), rootHash)
	return nil
}
