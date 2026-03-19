package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func runLs(cmd *cobra.Command, args []string) error {
	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	sandboxes, err := env.Runtime.List(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	if len(sandboxes) == 0 {
		fmt.Println("No sandboxes.")
		return nil
	}

	fmt.Printf("%-20s %-36s %-10s %-8s %-16s\n",
		"NAME", "ID", "STATE", "PID", "GUEST IP")
	fmt.Println(strings.Repeat("-", 94))

	for _, sb := range sandboxes {
		pid := ""
		if sb.PID > 0 {
			pid = fmt.Sprintf("%d", sb.PID)
		}
		fmt.Printf("%-20s %-36s %-10s %-8s %-16s\n",
			sb.Spec.Name, sb.Spec.ID, sb.State, pid, sb.GuestIP)
	}

	skills := env.Registry.List()
	if len(skills) > 0 {
		fmt.Printf("\nRegistered Skills:\n")
		fmt.Printf("%-20s %-10s %-4s %-12s\n", "NAME", "STATE", "VER", "HASH")
		fmt.Println(strings.Repeat("-", 50))
		for _, sk := range skills {
			fmt.Printf("%-20s %-10s %-4d %-12s\n",
				sk.Name, sk.State, sk.Version, sk.MerkleHash[:12])
		}
		fmt.Printf("Registry root: %s (seq %d)\n", env.Registry.RootHash()[:16], env.Registry.Sequence())
	}

	return nil
}
