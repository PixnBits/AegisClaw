// Package registry implements a read-only ClawHub-compatible skill registry
// client for AegisClaw.
//
// This is Phase 2, Task 6 of the OpenClaw integration plan.
//
// Any skill imported from the registry is automatically submitted to the
// Governance Court for full review (5-AI reviewers + SAST/SCA/secrets gates)
// before it can be activated.  No imported skill ever runs without Court
// approval — this is a hard invariant enforced at the import step.
//
// # Registry Protocol
//
// The registry exposes a simple JSON HTTP API (ClawHub-compatible):
//
//	GET  /v1/skills              → []SkillEntry
//	GET  /v1/skills/{name}       → SkillEntry
//	GET  /v1/skills/{name}/spec  → SkillSpec (JSON)
//
// All responses are validated for size and schema before use.  The client
// sets a conservative timeout and enforces a response-size cap to prevent
// abuse of the import flow.
package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultRegistryURL is the public ClawHub registry endpoint.
	defaultRegistryURL = "https://registry.clawhub.io"

	// maxResponseBytes caps registry responses to prevent memory exhaustion.
	maxResponseBytes = 512 * 1024 // 512 KiB

	// clientTimeout is the HTTP timeout for registry requests.
	clientTimeout = 15 * time.Second
)

// SkillEntry is a brief description of a skill in the registry.
type SkillEntry struct {
	// Name is the unique skill identifier (e.g. "weather-lookup").
	Name string `json:"name"`

	// Version is the semver string for this registry entry.
	Version string `json:"version"`

	// Description is a short human-readable description.
	Description string `json:"description"`

	// Author is the skill author's registry handle.
	Author string `json:"author,omitempty"`

	// Tags are optional classification labels.
	Tags []string `json:"tags,omitempty"`

	// PublishedAt is when this version was published.
	PublishedAt time.Time `json:"published_at,omitempty"`
}

// SkillSpec is the machine-readable definition of a skill as returned by the
// registry.  It is passed directly to proposal.create_draft when importing.
type SkillSpec struct {
	// Name is the unique skill name.
	Name string `json:"name"`

	// Description describes what the skill does.
	Description string `json:"description"`

	// Language is the implementation language ("python", "javascript", "bash", …).
	Language string `json:"language,omitempty"`

	// Tools lists the tool definitions the skill exposes.
	Tools []ToolDef `json:"tools,omitempty"`

	// SourceURL is a pointer to the canonical source (e.g. GitHub repo).
	SourceURL string `json:"source_url,omitempty"`

	// License is the SPDX license identifier.
	License string `json:"license,omitempty"`
}

// ToolDef describes one tool exposed by a skill.
type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Args        string `json:"args,omitempty"`
}

// Config holds the configuration for the registry client.
type Config struct {
	// URL is the base URL of the registry.  Defaults to defaultRegistryURL.
	URL string `yaml:"url" mapstructure:"url"`

	// InsecureSkipVerify disables TLS certificate verification.
	// Must never be true in production.  Exposed only for testing.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify" mapstructure:"insecure_skip_verify"`
}

// Client is a read-only registry client.
type Client struct {
	cfg    Config
	http   *http.Client
	base   string
}

// NewClient creates a new registry Client.  If cfg.URL is empty,
// defaultRegistryURL is used.
func NewClient(cfg Config) (*Client, error) {
	baseURL := cfg.URL
	if baseURL == "" {
		baseURL = defaultRegistryURL
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("registry: invalid URL %q: %w", baseURL, err)
	}
	// Strip trailing slash for consistent path construction.
	baseURL = strings.TrimRight(baseURL, "/")

	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: clientTimeout},
		base: baseURL,
	}, nil
}

// ListSkills returns all skills available in the registry.
func (c *Client) ListSkills(ctx context.Context) ([]SkillEntry, error) {
	var entries []SkillEntry
	if err := c.get(ctx, "/v1/skills", &entries); err != nil {
		return nil, fmt.Errorf("registry: list skills: %w", err)
	}
	return entries, nil
}

// FetchSkill returns the brief entry for a single named skill.
func (c *Client) FetchSkill(ctx context.Context, name string) (*SkillEntry, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("registry: skill name is required")
	}
	var entry SkillEntry
	if err := c.get(ctx, "/v1/skills/"+url.PathEscape(name), &entry); err != nil {
		return nil, fmt.Errorf("registry: fetch skill %q: %w", name, err)
	}
	return &entry, nil
}

// FetchSkillSpec returns the full machine-readable spec for a named skill.
// The caller should pass this spec directly to proposal.create_draft so the
// Governance Court can review it before activation.
func (c *Client) FetchSkillSpec(ctx context.Context, name string) (*SkillSpec, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("registry: skill name is required")
	}
	var spec SkillSpec
	if err := c.get(ctx, "/v1/skills/"+url.PathEscape(name)+"/spec", &spec); err != nil {
		return nil, fmt.Errorf("registry: fetch skill spec %q: %w", name, err)
	}
	if spec.Name == "" {
		return nil, fmt.Errorf("registry: skill spec for %q has no name field", name)
	}
	return &spec, nil
}

// get performs a GET request against the registry and decodes the JSON body
// into dest.  Response bodies are size-capped at maxResponseBytes.
func (c *Client) get(ctx context.Context, path string, dest interface{}) error {
	reqURL := c.base + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "AegisClaw-registry-client/1")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("not found (404)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)+1))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxResponseBytes {
		return fmt.Errorf("response too large (> %d bytes)", maxResponseBytes)
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}
	return nil
}
