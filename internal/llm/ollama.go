package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaEndpoint is the default local Ollama API endpoint.
const OllamaEndpoint = "http://127.0.0.1:11434"

// GenerateRequest is the payload sent to the Ollama /api/generate endpoint.
type GenerateRequest struct {
	Model       string         `json:"model"`
	Prompt      string         `json:"prompt"`
	System      string         `json:"system,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	Format      string         `json:"format,omitempty"`
	Stream      bool           `json:"stream"`
	Options     map[string]any `json:"options,omitempty"`
	Context     []int          `json:"context,omitempty"`
	KeepAlive   string         `json:"keep_alive,omitempty"`
}

// GenerateResponse is the response from the Ollama /api/generate endpoint.
type GenerateResponse struct {
	Model           string `json:"model"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	TotalDuration   int64  `json:"total_duration"`
	LoadDuration    int64  `json:"load_duration"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
	EvalDuration    int64  `json:"eval_duration"`
	Context         []int  `json:"context,omitempty"`
}

// ChatMessage represents a message in a chat conversation.
type ChatMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	Thinking string `json:"thinking,omitempty"`
}

// ChatRequest is the payload sent to the Ollama /api/chat endpoint.
type ChatRequest struct {
	Model       string         `json:"model"`
	Messages    []ChatMessage  `json:"messages"`
	Temperature float64        `json:"temperature,omitempty"`
	Format      string         `json:"format,omitempty"`
	Stream      bool           `json:"stream"`
	Options     map[string]any `json:"options,omitempty"`
	KeepAlive   string         `json:"keep_alive,omitempty"`
}

// ChatResponse is the response from the Ollama /api/chat endpoint.
type ChatResponse struct {
	Model           string      `json:"model"`
	Message         ChatMessage `json:"message"`
	Done            bool        `json:"done"`
	TotalDuration   int64       `json:"total_duration"`
	LoadDuration    int64       `json:"load_duration"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
	EvalDuration    int64       `json:"eval_duration"`
}

// ModelInfo represents information about a locally available model.
type ModelInfo struct {
	Name       string    `json:"name"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Digest     string    `json:"digest"`
}

// ListResponse is the response from the Ollama /api/tags endpoint.
type ListResponse struct {
	Models []ModelInfo `json:"models"`
}

// ShowRequest is the payload for /api/show.
type ShowRequest struct {
	Name string `json:"name"`
}

// ShowResponse is the detailed model info from /api/show.
type ShowResponse struct {
	ModelFile  string       `json:"modelfile"`
	Parameters string       `json:"parameters"`
	Template   string       `json:"template"`
	Details    ModelDetails `json:"details"`
}

// ModelDetails contains model metadata from /api/show.
type ModelDetails struct {
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// PullRequest is the payload for /api/pull.
type PullRequest struct {
	Name   string `json:"name"`
	Stream bool   `json:"stream"`
}

// PullResponse is the response from /api/pull (non-streaming).
type PullResponse struct {
	Status string `json:"status"`
	Digest string `json:"digest"`
	Total  int64  `json:"total"`
}

// ClientConfig configures the Ollama HTTP client.
type ClientConfig struct {
	// Endpoint is the Ollama API base URL (default: http://127.0.0.1:11434).
	Endpoint string
	// Timeout for HTTP requests (default: 5 minutes).
	Timeout time.Duration
	// HTTPClient, when provided, overrides the default transport. Tests use this
	// hook to replay recorded Ollama responses without touching production code.
	HTTPClient *http.Client
}

// Client communicates with a local Ollama instance.
type Client struct {
	endpoint string
	http     *http.Client
}

// NewClient creates a new Ollama client.
func NewClient(cfg ClientConfig) *Client {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = OllamaEndpoint
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	} else {
		cloned := *httpClient
		if cloned.Timeout == 0 {
			cloned.Timeout = timeout
		}
		httpClient = &cloned
	}
	return &Client{
		endpoint: endpoint,
		http:     httpClient,
	}
}

// Generate sends a completion request to Ollama.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal generate request: %w", err)
	}

	resp, err := c.post(ctx, "/api/generate", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode generate response: %w", err)
	}
	return &result, nil
}

// Chat sends a chat completion request to Ollama.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}

	resp, err := c.post(ctx, "/api/chat", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode chat response: %w", err)
	}
	return &result, nil
}

// List returns all locally available models.
func (c *Client) List(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("create list request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.readError(resp)
	}

	var result ListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	return result.Models, nil
}

// Show returns detailed information about a specific model.
func (c *Client) Show(ctx context.Context, name string) (*ShowResponse, error) {
	body, err := json.Marshal(ShowRequest{Name: name})
	if err != nil {
		return nil, fmt.Errorf("marshal show request: %w", err)
	}

	resp, err := c.post(ctx, "/api/show", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result ShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode show response: %w", err)
	}
	return &result, nil
}

// Pull downloads a model from the Ollama registry.
func (c *Client) Pull(ctx context.Context, name string) (*PullResponse, error) {
	body, err := json.Marshal(PullRequest{Name: name, Stream: false})
	if err != nil {
		return nil, fmt.Errorf("marshal pull request: %w", err)
	}

	resp, err := c.post(ctx, "/api/pull", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result PullResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode pull response: %w", err)
	}
	return &result, nil
}

// Healthy returns true if Ollama is reachable at the configured endpoint.
func (c *Client) Healthy(ctx context.Context) bool {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/", nil)
	if err != nil {
		return false
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Client) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request to %s: %w", path, err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.readError(resp)
	}
	return resp, nil
}

func (c *Client) readError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(data))
}
