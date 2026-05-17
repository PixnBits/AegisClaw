package store

import "context"

// StoreVM represents the future dedicated microVM that will own all persistent
// state (ProposalStore, PRStore, CompositionStore, MemoryStore, etc.).
//
// During the Minimal TCB refactor the Host Daemon no longer directly owns
// store initialization. Instead it will launch and monitor a StoreVM instance
// (initially a stub) which will eventually expose stores over a narrow vsock
// protocol to AegisHub and other components.
//
// This is intentionally minimal scaffolding — real VM launch, vsock transport,
// and full store migration are future work.
type StoreVM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// stubStoreVM is the transitional in-process implementation used while the
// real Store VM is being developed. It currently does nothing; stores are still
// created via store.NewLocal inside initRuntime for compatibility.
type stubStoreVM struct{}

func (s *stubStoreVM) Start(ctx context.Context) error { return nil }
func (s *stubStoreVM) Stop(ctx context.Context) error  { return nil }

// NewStubStoreVM returns the minimal stub implementation.
func NewStubStoreVM() StoreVM {
	return &stubStoreVM{}
}
