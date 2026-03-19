package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runSandboxDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	// TODO: Implement sandbox deletion
	fmt.Printf("Deleting sandbox '%s' not yet implemented.\n", name)
	return nil
}