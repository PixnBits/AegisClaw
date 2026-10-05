#!/bin/bash
# Deterministic scratch module. The agent adds Truncate; this seed does not.
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: e1.sh <dest_dir>" >&2
  exit 2
fi
dest=$1
mkdir -p "$dest"
cat > "$dest/go.mod" <<'EOF'
module scratch.example/e1

go 1.21
EOF
cat > "$dest/textutil.go" <<'EOF'
package textutil

// Repeat returns s concatenated n times.
// A non-positive n or an empty s yields an empty string.
func Repeat(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
EOF
cat > "$dest/textutil_test.go" <<'EOF'
package textutil

import "testing"

func TestRepeatTwice(t *testing.T) {
	if got := Repeat("ab", 2); got != "abab" {
		t.Fatalf("got %q", got)
	}
}

func TestRepeatZero(t *testing.T) {
	if got := Repeat("ab", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestRepeatEmpty(t *testing.T) {
	if got := Repeat("", 4); got != "" {
		t.Fatalf("got %q", got)
	}
}
EOF
