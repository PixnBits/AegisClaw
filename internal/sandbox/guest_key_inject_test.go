package sandbox

import "testing"

func TestNeedsPerVMRootfsOnDemandRoles(t *testing.T) {
	if !needsPerVMRootfs("agent-abc") || !needsPerVMRootfs("memory-abc") {
		t.Fatal("paired agent/memory must get a private rootfs")
	}
	if !needsPerVMRootfs("coder-tune-css-7") || !needsPerVMRootfs("tester-tune-css-7") {
		t.Fatal("on-demand coder/tester must not share agent.img")
	}
	if needsPerVMRootfs("court-persona-tester") || needsPerVMRootfs("project-manager-main") {
		t.Fatal("Court/PM shared images stay shared")
	}
}
