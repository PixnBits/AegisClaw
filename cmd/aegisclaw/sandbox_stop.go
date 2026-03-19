package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runSandboxStop(cmd *cobra.Command, args []string) error {
	name := args[0]
	// TODO: Implement sandbox stopping
	fmt.Printf("Stopping sandbox '%s' not yet implemented.\n", name)
	return nil
}