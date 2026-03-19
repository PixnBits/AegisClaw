package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func runSandboxStart(cmd *cobra.Command, args []string) error {
	name := args[0]
	// TODO: Implement sandbox starting
	fmt.Printf("Starting sandbox '%s' not yet implemented.\n", name)
	return nil
}