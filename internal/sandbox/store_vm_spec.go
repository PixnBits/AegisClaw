package sandbox

// StoreVMSpec for the real Firecracker Store microVM.
type StoreVMSpec struct {
	KernelImage     string
	RootfsPath      string
	CPUs            int
	MemoryMB        int
	VsockCID        uint32
	VsockPort       uint32
	DataDir         string // mounted persistent volume inside guest
	DataVolumePath  string // host path for persistent data
}

func DefaultStoreVMSpec() StoreVMSpec {
	return StoreVMSpec{
		KernelImage:    "/var/lib/aegisclaw/vmlinux-5.10.225",
		RootfsPath:   "/var/lib/aegisclaw/rootfs-templates/store.ext4",
		CPUs:         1,
		MemoryMB:     512,
		VsockCID:     3,
		VsockPort:    9999,
		DataDir:      "/data",
		DataVolumePath: "/var/lib/aegisclaw/data/store", // persistent on host
	}
}
