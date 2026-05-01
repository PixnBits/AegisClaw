# snapshot.go

## Purpose
Implements Firecracker microVM snapshot creation and restoration, enabling fast VM resume without cold-boot latency. Snapshots capture the full memory and machine state of a running VM. The create flow always resumes the VM after snapshotting (even on error), ensuring VMs are never left paused. Snapshot metadata is stored alongside the snapshot files and includes the original `SandboxSpec` for spec-accurate restoration.

## Key Types and Functions
- `SnapshotMeta`: Label, VMID, VMName, SnapFile (VM state), MemFile (guest memory), CreatedAt, OriginalSpec (`SandboxSpec`)
- `CreateSnapshot(ctx, machine, spec, label, dir) (*SnapshotMeta, error)`: pauses VM → creates snapshot files → resumes VM; always resumes even on snapshot error; writes `SnapshotMeta` JSON; audit-logged
- `RestoreSnapshot(ctx, meta, cfg) (*firecracker.Machine, error)`: reconstructs machine configuration from `SnapshotMeta.OriginalSpec`; loads snapshot state; starts the restored VM
- `LoadSnapshotMeta(path) (*SnapshotMeta, error)`: reads snapshot metadata JSON from disk
- `ListSnapshots(dir) ([]*SnapshotMeta, error)`: scans a directory for snapshot metadata files

## Role in the System
Used by `FirecrackerRuntime` to support VM checkpointing — enabling rapid scale-up of new skill sandboxes by restoring from a warm snapshot rather than cold-booting Alpine Linux each time.

## Dependencies
- `github.com/firecracker-microvm/firecracker-go-sdk`: VM pause/snapshot/resume operations
- `internal/kernel`: audit logging for snapshot events
- `encoding/json`: metadata serialisation
- `time`, `os`, `path/filepath`
