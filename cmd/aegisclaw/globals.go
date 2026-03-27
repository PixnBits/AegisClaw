package main

// Global flags shared across all commands, matching the CLI specification:
//   --json        Output in structured JSON (for scripting)
//   --verbose, -v Increase verbosity
//   --dry-run     Simulate action without making changes
//   --force       Skip confirmations (logged in audit trail)
var (
	globalJSON    bool
	globalVerbose bool
	globalDryRun  bool
	globalForce   bool
)
