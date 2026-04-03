package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
	log "github.com/sirupsen/logrus"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// SnapshotMeta holds metadata about a stored VM snapshot.
type SnapshotMeta struct {
	// Label is a human-readable identifier (e.g. "agent-baseline").
	Label string `json:"label"`
	// VMID is the ID of the VM that was snapshotted.
	VMID string `json:"vm_id"`
	// VMName is the friendly name of the snapshotted VM.
	VMName string `json:"vm_name"`
	// SnapFile is the path to the Firecracker VM state file.
	SnapFile string `json:"snap_file"`
	// MemFile is the path to the memory dump file.
	MemFile string `json:"mem_file"`
	// CreatedAt is when the snapshot was taken.
	CreatedAt time.Time `json:"created_at"`
	// OriginalSpec is the SandboxSpec of the VM at snapshot time.
	// It is used to reconstruct the Drives, network policy, and resources
	// when restoring the snapshot into a new VM.
	OriginalSpec SandboxSpec `json:"original_spec"`
}

// snapshotDir returns the directory for a snapshot with the given label.
func snapshotDir(baseDir, label string) string {
	return filepath.Join(baseDir, sanitizeID(label))
}

// CreateSnapshot pauses the running VM, writes memory + VM state to the
// snapshot directory, then resumes the VM.  The snapshot is annotated with
// the given label so it can be restored later via RestoreSnapshot.
//
// The caller must ensure that the snapshot base directory is set in
// RuntimeConfig.  Snapshot files are stored at:
//
//	<baseDir>/<label>/vm.snap   — Firecracker VM state
//	<baseDir>/<label>/mem.bin   — memory dump
//	<baseDir>/<label>/meta.json — SnapshotMeta (label, vmID, timestamps, spec)
func (r *FirecrackerRuntime) CreateSnapshot(ctx context.Context, vmID, label, baseDir string) (*SnapshotMeta, error) {
	r.mu.RLock()
	ms, exists := r.sandboxes[vmID]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("sandbox %s not found", vmID)
	}
	if ms.info.State != StateRunning {
		return nil, fmt.Errorf("sandbox %s is not running (state: %s)", vmID, ms.info.State)
	}
	if ms.machine == nil {
		return nil, fmt.Errorf("sandbox %s has no active machine handle", vmID)
	}

	snapDir := snapshotDir(baseDir, label)
	if err := os.MkdirAll(snapDir, 0700); err != nil {
		return nil, fmt.Errorf("create snapshot dir %s: %w", snapDir, err)
	}

	snapFile := filepath.Join(snapDir, "vm.snap")
	memFile := filepath.Join(snapDir, "mem.bin")

	// Pause the VM before capturing memory state.
	if err := ms.machine.PauseVM(ctx); err != nil {
		return nil, fmt.Errorf("pause VM %s before snapshot: %w", vmID, err)
	}

	snapErr := ms.machine.CreateSnapshot(ctx, memFile, snapFile)

	// Always resume — even on snapshot failure — so the VM isn't left paused.
	if resumeErr := ms.machine.ResumeVM(ctx); resumeErr != nil {
		r.logger.Error("failed to resume VM after snapshot attempt",
			zap.String("vm_id", vmID),
			zap.Error(resumeErr),
		)
	}

	if snapErr != nil {
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("create snapshot for VM %s: %w", vmID, snapErr)
	}

	meta := &SnapshotMeta{
		Label:        label,
		VMID:         vmID,
		VMName:       ms.info.Spec.Name,
		SnapFile:     snapFile,
		MemFile:      memFile,
		CreatedAt:    time.Now().UTC(),
		OriginalSpec: ms.info.Spec,
	}

	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal snapshot meta: %w", err)
	}
	metaFile := filepath.Join(snapDir, "meta.json")
	if err := os.WriteFile(metaFile, metaBytes, 0600); err != nil {
		return nil, fmt.Errorf("write snapshot meta: %w", err)
	}

	// Log snapshot creation in the Merkle audit trail.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"vm_id":    vmID,
		"label":    label,
		"snap_dir": snapDir,
	})
	auditAction := kernel.NewAction(kernel.ActionSnapshotCreate, "kernel", auditPayload)
	if _, signErr := r.kern.SignAndLog(auditAction); signErr != nil {
		r.logger.Error("failed to audit-log snapshot creation", zap.Error(signErr))
	}

	r.logger.Info("VM snapshot created",
		zap.String("vm_id", vmID),
		zap.String("label", label),
		zap.String("snap_dir", snapDir),
	)
	return meta, nil
}

// LoadSnapshotMeta reads the SnapshotMeta from a snapshot directory.
// Returns an error if the snapshot does not exist or the metadata is corrupt.
func LoadSnapshotMeta(baseDir, label string) (*SnapshotMeta, error) {
	metaFile := filepath.Join(snapshotDir(baseDir, label), "meta.json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("snapshot %q not found in %s", label, baseDir)
		}
		return nil, fmt.Errorf("read snapshot meta: %w", err)
	}
	var meta SnapshotMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse snapshot meta: %w", err)
	}
	return &meta, nil
}

// ListSnapshots returns metadata for all snapshots stored in baseDir.
func ListSnapshots(baseDir string) ([]*SnapshotMeta, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list snapshot dir %s: %w", baseDir, err)
	}

	var out []*SnapshotMeta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		meta, err := LoadSnapshotMeta(baseDir, e.Name())
		if err != nil {
			continue // skip corrupt/incomplete snapshots
		}
		out = append(out, meta)
	}
	return out, nil
}

// RestoreSnapshot creates a new Firecracker VM restored from the snapshot
// identified by label.  The new VM is registered under newSpec.ID and started
// immediately.  The original spec (drives, resources, vsock, network policy)
// is inherited from the snapshot metadata; callers may override by modifying
// newSpec before calling.
//
// Returns the ID of the newly created VM.
func (r *FirecrackerRuntime) RestoreSnapshot(ctx context.Context, meta *SnapshotMeta, newSpec SandboxSpec) (string, error) {
	// Assign CID and validate spec.
	r.mu.Lock()
	if newSpec.VsockCID < minVsockCID {
		newSpec.VsockCID = r.allocateCID()
	}
	r.mu.Unlock()

	if err := newSpec.Validate(); err != nil {
		return "", fmt.Errorf("invalid sandbox spec for snapshot restore: %w", err)
	}

	r.mu.Lock()
	if _, exists := r.sandboxes[newSpec.ID]; exists {
		r.mu.Unlock()
		return "", fmt.Errorf("sandbox %s already exists", newSpec.ID)
	}

	sandboxDir := filepath.Join(r.cfg.StateDir, newSpec.ID)
	r.mu.Unlock()

	if err := os.MkdirAll(sandboxDir, 0700); err != nil {
		return "", fmt.Errorf("create sandbox dir for restore: %w", err)
	}

	socketPath := filepath.Join(sandboxDir, "firecracker.sock")
	os.Remove(socketPath)

	effectiveSocketPath := socketPath
	if _, err := os.Stat(r.cfg.JailerBin); err == nil {
		effectiveSocketPath = "api.sock"
	}

	// Build Firecracker config with snapshot load path.
	fcCfg := firecracker.Config{
		SocketPath: effectiveSocketPath,
		Snapshot: firecracker.SnapshotConfig{
			SnapshotPath: meta.SnapFile,
			MemFilePath:  meta.MemFile,
			ResumeVM:     true,
		},
		// vsock so the daemon can communicate with the restored agent.
		VsockDevices: []firecracker.VsockDevice{
			{
				ID:   "vsock0",
				Path: filepath.Join(sandboxDir, "vsock.sock"),
				CID:  newSpec.VsockCID,
			},
		},
		Seccomp: firecracker.SeccompConfig{
			Enabled: true,
		},
	}

	vmCtx, vmCancel := context.WithCancel(context.Background())
	logEntry := log.NewEntry(log.New())
	logEntry.Logger.SetLevel(log.WarnLevel)

	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(logEntry.WithField("sandbox_id", newSpec.ID)),
	}

	jailerCfg := r.buildJailerConfig(newSpec)
	if _, err := os.Stat(r.cfg.JailerBin); err == nil {
		fcCfg.JailerCfg = &jailerCfg
	}

	machine, err := firecracker.NewMachine(vmCtx, fcCfg, machineOpts...)
	if err != nil {
		vmCancel()
		return "", fmt.Errorf("create Firecracker machine for snapshot restore %s: %w", newSpec.ID, err)
	}
	if err := machine.Start(vmCtx); err != nil {
		vmCancel()
		return "", fmt.Errorf("start restored VM %s: %w", newSpec.ID, err)
	}

	now := time.Now().UTC()
	info := SandboxInfo{
		Spec:      newSpec,
		State:     StateRunning,
		StartedAt: &now,
	}
	if pid, pidErr := machine.PID(); pidErr == nil {
		info.PID = pid
	}

	r.mu.Lock()
	r.sandboxes[newSpec.ID] = &managedSandbox{
		info:    info,
		machine: machine,
		cancel:  vmCancel,
	}
	r.mu.Unlock()

	// Audit-log the restore.
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"vm_id":          newSpec.ID,
		"snapshot_label": meta.Label,
		"original_vm_id": meta.VMID,
	})
	auditAction := kernel.NewAction(kernel.ActionSnapshotRestore, "kernel", auditPayload)
	if _, signErr := r.kern.SignAndLog(auditAction); signErr != nil {
		r.logger.Error("failed to audit-log snapshot restore", zap.Error(signErr))
	}

	if err := r.saveState(); err != nil {
		r.logger.Error("failed to persist state after snapshot restore", zap.Error(err))
	}

	r.logger.Info("VM restored from snapshot",
		zap.String("vm_id", newSpec.ID),
		zap.String("snapshot_label", meta.Label),
	)
	return newSpec.ID, nil
}
