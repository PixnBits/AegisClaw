package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func runSandboxDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	env, err := initRuntime()
	if err != nil {
		return err
	}
	defer env.Logger.Sync()

	ctx := context.Background()

	id, err := resolveNameToID(env, ctx, name)
	if err != nil {
		return err
	}

	if err := env.Runtime.Delete(ctx, id); err != nil {
		return fmt.Errorf("failed to delete sandbox: %w", err)
	}

	// Deactivate in registry if registered as a skill
	skills := env.Registry.List()
	for _, sk := range skills {
		if sk.SandboxID == id {
			env.Registry.Deactivate(sk.Name)
		}
	}

	fmt.Printf("Sandbox '%s' deleted.\n", name)
	return nil
}
