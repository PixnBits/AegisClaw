package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, set via -ldflags at build time.
var (
	buildCommit = "dev"
	buildDate   = "unknown"
)

func runVersion(cmd *cobra.Command, args []string) {
	if globalJSON {
		data, _ := json.MarshalIndent(map[string]string{
			"version": version,
			"commit":  buildCommit,
			"date":    buildDate,
			"go":      runtime.Version(),
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		}, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("AegisClaw %s\n", version)
	fmt.Printf("  Commit:  %s\n", buildCommit)
	fmt.Printf("  Built:   %s\n", buildDate)
	fmt.Printf("  Go:      %s\n", runtime.Version())
	fmt.Printf("  OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
