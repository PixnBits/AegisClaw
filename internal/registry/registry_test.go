package registry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient_DefaultURL(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.base != defaultRegistryURL {
		t.Errorf("expected base %q, got %q", defaultRegistryURL, c.base)
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient(Config{URL: "not a url"})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestNewClient_TrailingSlashStripped(t *testing.T) {
	c, err := NewClient(Config{URL: "https://example.com/registry/"})
	if err != nil {
		t.Fatal(err)
	}
	if c.base != "https://example.com/registry" {
		t.Errorf("expected trailing slash stripped, got %q", c.base)
	}
}

func TestListSkills(t *testing.T) {
	entries := []SkillEntry{
		{Name: "skill-a", Version: "1.0.0", Description: "skill A", PublishedAt: time.Now()},
		{Name: "skill-b", Version: "2.0.0", Description: "skill B"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/skills" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries) //nolint:errcheck
	}))
	defer srv.Close()

	c, err := NewClient(Config{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.ListSkills(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Name != "skill-a" {
		t.Errorf("expected skill-a, got %q", got[0].Name)
	}
}

func TestFetchSkill_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{URL: srv.URL})
	_, err := c.FetchSkill(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestFetchSkill_EmptyName(t *testing.T) {
	c, _ := NewClient(Config{})
	_, err := c.FetchSkill(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestFetchSkillSpec_Success(t *testing.T) {
	spec := SkillSpec{
		Name:        "weather-lookup",
		Description: "Look up current weather",
		Language:    "python",
		Tools: []ToolDef{
			{Name: "get_weather", Description: "Get weather for a city"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/skills/weather-lookup/spec" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(spec) //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, _ := NewClient(Config{URL: srv.URL})
	got, err := c.FetchSkillSpec(context.Background(), "weather-lookup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "weather-lookup" {
		t.Errorf("expected name 'weather-lookup', got %q", got.Name)
	}
	if len(got.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(got.Tools))
	}
}

func TestGet_ResponseTooLarge(t *testing.T) {
	large := make([]byte, maxResponseBytes+100)
	for i := range large {
		large[i] = 'x'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(large) //nolint:errcheck
	}))
	defer srv.Close()

	c, _ := NewClient(Config{URL: srv.URL})
	_, err := c.ListSkills(context.Background())
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
}
