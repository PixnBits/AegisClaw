package sandbox

// AegisHubVMSpec defines Firecracker configuration for AegisHub.
type AegisHubVMSpec struct {
	KernelImage string
	RootfsPath  string
	CPUs        int
	MemoryMB    int
	VsockCID    uint32
	VsockPort   uint32
}

func DefaultAegisHubVMSpec() AegisHubVMSpec {
	return AegisHubVMSpec{
		KernelImage: "/var/lib/aegisclaw/vmlinux-5.10.225",
		RootfsPath:  "/var/lib/aegisclaw/rootfs-templates/aegishub.ext4",
		CPUs:        2,
		MemoryMB:    1024,
		VsockCID:    2,
		VsockPort:   9998,
	}
}
