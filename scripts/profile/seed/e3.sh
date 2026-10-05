#!/bin/bash
# Deterministic scratch CLI. Flags, defaults, and env vars live only in main.go.
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: e3.sh <dest_dir>" >&2
  exit 2
fi
dest=$1
mkdir -p "$dest"
cat > "$dest/go.mod" <<'EOF'
module scratch.example/e3

go 1.21
EOF
cat > "$dest/main.go" <<'EOF'
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	port := flag.Int("port", 8080, "TCP port to listen on")
	name := flag.String("name", "scratch", "display name")
	verbose := flag.Bool("verbose", false, "emit extra diagnostics")
	flag.Parse()

	level := os.Getenv("LOG_LEVEL")
	if level == "" {
		level = "info"
	}
	out := os.Getenv("OUTPUT_PATH")
	if out == "" {
		out = "stdout"
	}
	fmt.Printf("name=%s port=%d verbose=%t level=%s out=%s\n", *name, *port, *verbose, level, out)
}
EOF
cat > "$dest/README.md" <<'EOF'
# scratch

A small command. The usage section has not been written.
EOF
