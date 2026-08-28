package runtime

import "testing"

func TestIsProjectManagerVM(t *testing.T) {
	if !isProjectManagerVM("project-manager", "project-manager-css-r1") {
		t.Fatal("type+id must match")
	}
	if !isProjectManagerVM("agent", "project-manager-main") {
		t.Fatal("id prefix must match")
	}
	if isProjectManagerVM("agent", "coder-css-r1") {
		t.Fatal("coder must not use the PM model")
	}
	if isProjectManagerVM("court-persona", "court-persona-ciso") {
		t.Fatal("court must not use the PM model")
	}
}
