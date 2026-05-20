package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// forbiddenLegacyMigrationSymbols must never reappear in internal/config
// non-test Go sources (DB-08). They were removed with the directory-layout
// simplification; reintroduction would silently rewrite user paths again.
var forbiddenLegacyMigrationSymbols = []string{
	"normalizeConfigPaths",
	"migrateLegacyPath",
	"migrateLegacy",
}

func TestConfigSourcesDoNotReintroduceLegacyPathMigration(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(ent.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", path, err)
		}
		text := string(body)
		for _, sym := range forbiddenLegacyMigrationSymbols {
			if strings.Contains(text, sym) {
				t.Errorf("%s: forbidden legacy migration symbol %q must stay removed", ent.Name(), sym)
			}
		}
	}
}
