package sandbox

// StoreVMSpec defines the Firecracker configuration for the Store microVM.
// This is the start of the real Firecracker Store VM implementation.
type StoreVMSpec struct {
	KernelImage     string
	RootfsPath      string
	CPUs            int
	MemoryMB        int
	VsockCID        uint32
	VsockPort       uint32
	DataDir         string // where persistent stores live inside the VM
}

// DefaultStoreVMSpec returns a reasonable default spec for the Store VM.
func DefaultStoreVMSpec() StoreVMSpec {
	return StoreVMSpec{
		KernelImage: "/var/lib/aegisclaw/vmlinux-5.10.225",
		RootfsPath:  "/var/lib/aegisclaw/rootfs-templates/store.ext4", // to be built
		CPUs:        1,
		MemoryMB:    512,
		VsockCID:    3,   // dedicated CID for Store VM
		VsockPort:   9999,
		DataDir:     "/data",
	}
}
