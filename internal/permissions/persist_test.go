package permissions

import (
	"os"
	"testing"
)

func TestLoadState_PreservesEmptyGrants(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	data := `{"version":1,"grants":[],"visibility":[],"requests":[]}`
	if err := os.WriteFile(permissionsFile, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	s := LoadState()
	if s == nil {
		t.Fatal("expected non-nil state")
	}
	if len(s.Grants) != 0 {
		t.Fatalf("expected empty grants preserved, got %d", len(s.Grants))
	}
}