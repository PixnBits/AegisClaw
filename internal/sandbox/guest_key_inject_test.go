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

func TestNeedsPerVMRootfsBuilder(t *testing.T) {
	if !needsPerVMRootfs("builder-1") {
		t.Fatal("builder-1 must get a private rootfs")
	}
	if !needsPerVMRootfs("builder") {
		t.Fatal("id/type builder must get a private rootfs")
	}
	if needsPerVMRootfs("store") {
		t.Fatal("store must keep the shared template")
	}
}
