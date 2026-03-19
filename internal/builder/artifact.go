package builder

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PixnBits/AegisClaw/internal/kernel"
	"go.uber.org/zap"
)

// ArtifactType classifies the kind of artifact produced.
type ArtifactType string

const (
	ArtifactTypeBinary   ArtifactType = "binary"
	ArtifactTypeManifest ArtifactType = "manifest"
	ArtifactTypeSource   ArtifactType = "source"
)

// ArtifactManifest describes a signed build artifact and its sandbox-ready metadata.
type ArtifactManifest struct {
	SkillID       string            `json:"skill_id"`
	ProposalID    string            `json:"proposal_id"`
	Version       string            `json:"version"`
	CommitHash    string            `json:"commit_hash"`
	BinaryPath    string            `json:"binary_path"`
	BinaryHash    string            `json:"binary_hash"`
	BinarySize    int64             `json:"binary_size"`
	FileHashes    map[string]string `json:"file_hashes"`
	EntryPoint    string            `json:"entry_point"`
	Language      string            `json:"language"`
	BuildMode     string            `json:"build_mode"`
	Signature     string            `json:"signature"`
	SignedAt      time.Time         `json:"signed_at"`
	KernelPubKey  string            `json:"kernel_pub_key"`
	Sandbox       SandboxManifest   `json:"sandbox"`
}

// SandboxManifest contains sandbox-ready deployment metadata.
type SandboxManifest struct {
	VCPUs         int      `json:"vcpus"`
	MemoryMB      int      `json:"memory_mb"`
	DiskMB        int      `json:"disk_mb"`
	NetworkPolicy string   `json:"network_policy"`
	SecretsRefs   []string `json:"secrets_refs,omitempty"`
	ReadOnlyRoot  bool     `json:"read_only_root"`
	EntryCommand  string   `json:"entry_command"`
}

// Validate checks the artifact manifest has required fields.
func (am *ArtifactManifest) Validate() error {
	if am.SkillID == "" {
		return fmt.Errorf("skill ID is required")
	}
	if am.ProposalID == "" {
		return fmt.Errorf("proposal ID is required")
	}
	if am.Version == "" {
		return fmt.Errorf("version is required")
	}
	if am.BinaryHash == "" {
		return fmt.Errorf("binary hash is required")
	}
	if am.BinarySize <= 0 {
		return fmt.Errorf("binary size must be positive")
	}
	if am.Signature == "" {
		return fmt.Errorf("signature is required")
	}
	return nil
}

// ArtifactStore manages signed build artifacts on disk.
type ArtifactStore struct {
	baseDir string
	kern    *kernel.Kernel
	logger  *zap.Logger
	mu      sync.Mutex
}

// NewArtifactStore creates an ArtifactStore at the given base directory.
func NewArtifactStore(baseDir string, kern *kernel.Kernel, logger *zap.Logger) (*ArtifactStore, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("base directory is required")
	}
	if kern == nil {
		return nil, fmt.Errorf("kernel is required")
	}

	// Ensure base directory exists
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create artifacts directory %s: %w", baseDir, err)
	}

	return &ArtifactStore{
		baseDir: baseDir,
		kern:    kern,
		logger:  logger,
	}, nil
}

// PackageArtifact signs the binary and manifest, then stores them under artifacts/<skill-id>/.
func (as *ArtifactStore) PackageArtifact(
	skillID string,
	proposalID string,
	version string,
	commitHash string,
	binaryData []byte,
	fileHashes map[string]string,
	spec *SkillSpec,
) (*ArtifactManifest, error) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if skillID == "" {
		return nil, fmt.Errorf("skill ID is required")
	}
	if proposalID == "" {
		return nil, fmt.Errorf("proposal ID is required")
	}
	if len(binaryData) == 0 {
		return nil, fmt.Errorf("binary data is required")
	}
	if spec == nil {
		return nil, fmt.Errorf("skill spec is required")
	}

	// Security: validate skill ID to prevent path traversal
	cleanID := filepath.Clean(skillID)
	if cleanID != skillID || strings.Contains(skillID, "..") || strings.Contains(skillID, "/") {
		return nil, fmt.Errorf("invalid skill ID: %q", skillID)
	}

	// Create skill artifact directory
	skillDir := filepath.Join(as.baseDir, skillID)
	if err := os.MkdirAll(skillDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create skill directory: %w", err)
	}

	// Compute SHA-256 of the binary
	binarySum := sha256.Sum256(binaryData)
	binaryHash := hex.EncodeToString(binarySum[:])

	// Sign the binary with the kernel's Ed25519 key
	signature := as.kern.Sign(binaryData)
	sigHex := hex.EncodeToString(signature)

	// Build the manifest
	manifest := &ArtifactManifest{
		SkillID:    skillID,
		ProposalID: proposalID,
		Version:    version,
		CommitHash: commitHash,
		BinaryPath: filepath.Join(skillID, "skill"),
		BinaryHash: binaryHash,
		BinarySize: int64(len(binaryData)),
		FileHashes: fileHashes,
		EntryPoint: spec.EntryPoint,
		Language:   spec.Language,
		BuildMode:  "pie",
		Signature:  sigHex,
		SignedAt:   time.Now().UTC(),
		KernelPubKey: hex.EncodeToString(as.kern.PublicKey()),
		Sandbox: SandboxManifest{
			VCPUs:        1,
			MemoryMB:     256,
			DiskMB:       128,
			NetworkPolicy: formatNetworkPolicy(spec.NetworkPolicy),
			SecretsRefs:  spec.SecretsRefs,
			ReadOnlyRoot: true,
			EntryCommand: "./" + filepath.Base(spec.EntryPoint),
		},
	}

	// Write binary
	binaryPath := filepath.Join(skillDir, "skill")
	if err := os.WriteFile(binaryPath, binaryData, 0o640); err != nil {
		return nil, fmt.Errorf("failed to write binary: %w", err)
	}

	// Write manifest
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(skillDir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestData, 0o640); err != nil {
		return nil, fmt.Errorf("failed to write manifest: %w", err)
	}

	// Sign the manifest itself and write the signature
	manifestSig := as.kern.Sign(manifestData)
	sigPath := filepath.Join(skillDir, "manifest.sig")
	if err := os.WriteFile(sigPath, manifestSig, 0o640); err != nil {
		return nil, fmt.Errorf("failed to write manifest signature: %w", err)
	}

	// Write SHA-256 checksum file
	checksumContent := fmt.Sprintf("%s  skill\n%s  manifest.json\n",
		binaryHash, hex.EncodeToString(sha256Sum(manifestData)))
	checksumPath := filepath.Join(skillDir, "SHA256SUMS")
	if err := os.WriteFile(checksumPath, []byte(checksumContent), 0o640); err != nil {
		return nil, fmt.Errorf("failed to write checksums: %w", err)
	}

	// Audit log
	auditPayload, _ := json.Marshal(map[string]interface{}{
		"skill_id":    skillID,
		"proposal_id": proposalID,
		"version":     version,
		"commit":      commitHash,
		"binary_hash": binaryHash,
		"binary_size": len(binaryData),
	})
	action := kernel.NewAction(kernel.ActionBuilderBuild, "artifact-store", auditPayload)
	if _, logErr := as.kern.SignAndLog(action); logErr != nil {
		as.logger.Error("failed to log artifact packaging", zap.Error(logErr))
	}

	as.logger.Info("artifact packaged and signed",
		zap.String("skill_id", skillID),
		zap.String("proposal_id", proposalID),
		zap.String("binary_hash", binaryHash[:16]+"..."),
		zap.Int64("binary_size", manifest.BinarySize),
	)

	return manifest, nil
}

// VerifyArtifact verifies the binary's signature and hash integrity.
func (as *ArtifactStore) VerifyArtifact(skillID string) (*ArtifactManifest, error) {
	cleanID := filepath.Clean(skillID)
	if cleanID != skillID || strings.Contains(skillID, "..") || strings.Contains(skillID, "/") {
		return nil, fmt.Errorf("invalid skill ID: %q", skillID)
	}

	skillDir := filepath.Join(as.baseDir, skillID)

	// Read and parse manifest
	manifestData, err := os.ReadFile(filepath.Join(skillDir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ArtifactManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("invalid manifest: %w", err)
	}

	// Verify manifest signature
	manifestSig, err := os.ReadFile(filepath.Join(skillDir, "manifest.sig"))
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest signature: %w", err)
	}

	if !as.kern.Verify(manifestData, manifestSig) {
		return nil, fmt.Errorf("manifest signature verification failed for skill %s", skillID)
	}

	// Read binary and verify hash
	binaryData, err := os.ReadFile(filepath.Join(skillDir, "skill"))
	if err != nil {
		return nil, fmt.Errorf("failed to read binary: %w", err)
	}

	actualHash := hex.EncodeToString(sha256Sum(binaryData))
	if actualHash != manifest.BinaryHash {
		return nil, fmt.Errorf("binary hash mismatch: expected %s, got %s", manifest.BinaryHash, actualHash)
	}

	// Verify binary signature
	sigBytes, err := hex.DecodeString(manifest.Signature)
	if err != nil {
		return nil, fmt.Errorf("failed to decode binary signature: %w", err)
	}

	if !as.kern.Verify(binaryData, sigBytes) {
		return nil, fmt.Errorf("binary signature verification failed for skill %s", skillID)
	}

	as.logger.Info("artifact verified",
		zap.String("skill_id", skillID),
		zap.String("binary_hash", manifest.BinaryHash[:16]+"..."),
	)

	return &manifest, nil
}

// ListArtifacts returns all skill IDs that have artifacts.
func (as *ArtifactStore) ListArtifacts() ([]string, error) {
	entries, err := os.ReadDir(as.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read artifacts directory: %w", err)
	}

	var skills []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Check for manifest.json
		manifestPath := filepath.Join(as.baseDir, entry.Name(), "manifest.json")
		if _, err := os.Stat(manifestPath); err == nil {
			skills = append(skills, entry.Name())
		}
	}
	return skills, nil
}

// GetManifest reads and returns the manifest for a specific skill.
func (as *ArtifactStore) GetManifest(skillID string) (*ArtifactManifest, error) {
	cleanID := filepath.Clean(skillID)
	if cleanID != skillID || strings.Contains(skillID, "..") || strings.Contains(skillID, "/") {
		return nil, fmt.Errorf("invalid skill ID: %q", skillID)
	}

	manifestPath := filepath.Join(as.baseDir, skillID, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest for %s: %w", skillID, err)
	}

	var manifest ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest for %s: %w", skillID, err)
	}

	return &manifest, nil
}

// sha256Sum returns the SHA-256 digest of data.
func sha256Sum(data []byte) []byte {
	sum := sha256.Sum256(data)
	return sum[:]
}

// formatNetworkPolicy returns a string representation of the network policy.
func formatNetworkPolicy(np SkillNetworkPolicy) string {
	if np.DefaultDeny {
		if len(np.AllowedHosts) == 0 {
			return "default-deny"
		}
		return fmt.Sprintf("deny-except:%s", strings.Join(np.AllowedHosts, ","))
	}
	return "allow-all"
}
