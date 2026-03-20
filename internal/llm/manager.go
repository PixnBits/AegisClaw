package llm

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

// KnownGoodModels is the hardcoded list of verified model digests.
// Each entry maps an Ollama model name to its expected SHA256 digest prefix.
// Update this list when officially approving new model versions.
var KnownGoodModels = []ModelEntry{
	{Name: "mistral-nemo", SHA256: "", Tags: []string{"security", "code_review", "architecture"}},
	{Name: "llama3.2:3b", SHA256: "", Tags: []string{"code_review", "testing", "usability"}},
	{Name: "qwen2.5-coder:14b", SHA256: "", Tags: []string{"code_generation", "code_review"}},
	{Name: "nemotron-mini", SHA256: "", Tags: []string{"code_review", "testing"}},
}

// ManagerConfig configures the model manager.
type ManagerConfig struct {
	// ModelDir is the read-only directory for model storage (shared with reviewer sandboxes).
	ModelDir string
}

// Manager handles model lifecycle: listing, verification, and updates.
type Manager struct {
	client   *Client
	registry *ModelRegistry
	config   ManagerConfig
	logger   *zap.Logger
}

// NewManager creates a new model manager.
func NewManager(client *Client, registry *ModelRegistry, cfg ManagerConfig, logger *zap.Logger) *Manager {
	return &Manager{
		client:   client,
		registry: registry,
		config:   cfg,
		logger:   logger,
	}
}

// ModelStatus represents the current state of a model.
type ModelStatus struct {
	Name         string
	Registered   bool
	Available    bool
	Digest       string
	ExpectedHash string
	Verified     bool
	Size         int64
	Tags         []string
	Details      *ModelDetails
}

// ListStatus returns the combined status of all known models.
func (m *Manager) ListStatus(ctx context.Context) ([]ModelStatus, error) {
	// Get locally available models from Ollama
	available, err := m.client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list available models: %w", err)
	}
	availMap := make(map[string]ModelInfo, len(available))
	for _, info := range available {
		// Normalize: strip tag from name for matching ("mistral-nemo:latest" -> "mistral-nemo")
		base := normalizeModelName(info.Name)
		availMap[base] = info
	}

	// Merge registry entries with availability
	registered := m.registry.List()
	seen := make(map[string]bool, len(registered))

	var statuses []ModelStatus

	for _, entry := range registered {
		base := normalizeModelName(entry.Name)
		seen[base] = true

		status := ModelStatus{
			Name:         entry.Name,
			Registered:   true,
			ExpectedHash: entry.SHA256,
			Tags:         entry.Tags,
		}

		if info, ok := availMap[base]; ok {
			status.Available = true
			status.Digest = info.Digest
			status.Size = info.Size
			if entry.SHA256 != "" {
				status.Verified = strings.HasPrefix(info.Digest, "sha256:"+entry.SHA256) || info.Digest == entry.SHA256
			}
		}

		statuses = append(statuses, status)
	}

	// Add models available in Ollama but not registered
	for _, info := range available {
		base := normalizeModelName(info.Name)
		if seen[base] {
			continue
		}
		statuses = append(statuses, ModelStatus{
			Name:      info.Name,
			Available: true,
			Digest:    info.Digest,
			Size:      info.Size,
		})
	}

	return statuses, nil
}

// Verify checks a specific model's digest against its registered hash.
func (m *Manager) Verify(ctx context.Context, name string) (*ModelStatus, error) {
	entry, registered := m.registry.Get(name)

	info, err := m.client.Show(ctx, name)
	if err != nil {
		return &ModelStatus{
			Name:       name,
			Registered: registered,
			Available:  false,
		}, fmt.Errorf("model %q not available in Ollama: %w", name, err)
	}

	status := &ModelStatus{
		Name:       name,
		Registered: registered,
		Available:  true,
		Details:    &info.Details,
	}

	if registered {
		status.ExpectedHash = entry.SHA256
		status.Tags = entry.Tags
	}

	// Get digest from list (Show doesn't return digest directly)
	models, err := m.client.List(ctx)
	if err == nil {
		for _, mi := range models {
			if normalizeModelName(mi.Name) == normalizeModelName(name) {
				status.Digest = mi.Digest
				status.Size = mi.Size
				break
			}
		}
	}

	if registered && entry.SHA256 != "" && status.Digest != "" {
		status.Verified = strings.HasPrefix(status.Digest, "sha256:"+entry.SHA256) || status.Digest == entry.SHA256
	}

	m.logger.Info("model verification",
		zap.String("model", name),
		zap.Bool("registered", registered),
		zap.Bool("available", true),
		zap.Bool("verified", status.Verified),
		zap.String("digest", status.Digest),
	)

	return status, nil
}

// Update pulls a model and registers it with the current digest.
func (m *Manager) Update(ctx context.Context, name string) (*ModelStatus, error) {
	m.logger.Info("pulling model", zap.String("model", name))

	pullResp, err := m.client.Pull(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("pull model %q: %w", name, err)
	}

	m.logger.Info("model pulled",
		zap.String("model", name),
		zap.String("status", pullResp.Status),
		zap.String("digest", pullResp.Digest),
	)

	// Determine tags from known-good list or existing entry
	tags := tagsForModel(name)
	if existing, ok := m.registry.Get(name); ok {
		tags = existing.Tags
	}

	// Extract hash from digest (remove "sha256:" prefix if present)
	hash := pullResp.Digest
	if strings.HasPrefix(hash, "sha256:") {
		hash = hash[7:]
	}

	if err := m.registry.Register(ModelEntry{
		Name:   name,
		SHA256: hash,
		Tags:   tags,
	}); err != nil {
		return nil, fmt.Errorf("register model: %w", err)
	}

	return &ModelStatus{
		Name:       name,
		Registered: true,
		Available:  true,
		Digest:     pullResp.Digest,
		Verified:   true,
		Tags:       tags,
	}, nil
}

// SyncKnownGood ensures all known-good models are registered (without pulling).
// Existing entries are not overwritten.
func (m *Manager) SyncKnownGood() {
	for _, known := range KnownGoodModels {
		if _, ok := m.registry.Get(known.Name); !ok {
			m.registry.registerSeed(known)
		}
	}
}

func normalizeModelName(name string) string {
	// Strip ":latest" or other tags for comparison
	if idx := strings.LastIndex(name, ":"); idx > 0 {
		tag := name[idx+1:]
		if tag == "latest" {
			return name[:idx]
		}
	}
	return name
}

func tagsForModel(name string) []string {
	base := normalizeModelName(name)
	for _, known := range KnownGoodModels {
		if normalizeModelName(known.Name) == base {
			return known.Tags
		}
	}
	return nil
}
