#!/bin/bash
# Deterministic scratch module whose unit test fails because Clamp is wrong.
set -euo pipefail
if [[ $# -ne 1 ]]; then
  echo "usage: e2.sh <dest_dir>" >&2
  exit 2
fi
dest=$1
mkdir -p "$dest"
cat > "$dest/go.mod" <<'EOF'
module scratch.example/e2

go 1.21
EOF
cat > "$dest/clamp.go" <<'EOF'
package stats

// Clamp returns v limited to the inclusive range [lo, hi].
// lo is less than or equal to hi.
func Clamp(v, lo, hi int) int {
	if v < lo {
		return hi
	}
	if v > hi {
		return lo
	}
	return v
}
EOF
cat > "$dest/clamp_test.go" <<'EOF'
package stats

import "testing"

func TestClampInside(t *testing.T) {
	if got := Clamp(5, 1, 10); got != 5 {
		t.Fatalf("Clamp(5, 1, 10) = %d", got)
	}
}

func TestClampLow(t *testing.T) {
	if got := Clamp(0, 1, 10); got != 1 {
		t.Fatalf("Clamp(0, 1, 10) = %d", got)
	}
}

func TestClampHigh(t *testing.T) {
	if got := Clamp(12, 1, 10); got != 10 {
		t.Fatalf("Clamp(12, 1, 10) = %d", got)
	}
}
EOF
