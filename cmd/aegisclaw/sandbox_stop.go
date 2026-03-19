package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func runSandboxStop(cmd *cobra.Command, args []string) error {
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

	if err := env.Runtime.Stop(ctx, id); err != nil {
		return fmt.Errorf("failed to stop sandbox: %w", err)
	}

	fmt.Printf("Sandbox '%s' stopped.\n", name)
	return nil
}

func resolveNameToID(env *runtimeEnv, ctx context.Context, name string) (string, error) {
	sandboxes, err := env.Runtime.List(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list sandboxes: %w", err)
	}
	for _, sb := range sandboxes {
		if sb.Spec.Name == name {
			return sb.Spec.ID, nil
		}
		if sb.Spec.ID == name {
			return sb.Spec.ID, nil
		}
	}
	return "", fmt.Errorf("sandbox %q not found", name)
}
