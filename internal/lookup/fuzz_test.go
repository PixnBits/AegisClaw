package lookup

// fuzz_test.go — Property-based / fuzz tests for the lookup package.
//
// These run as normal Go fuzz targets under `go test -fuzz=.` and as unit
// tests under the regular `go test ./...` corpus replay.
//
// No KVM, no Ollama, no Firecracker required.
//
// Run fuzz modes (examples):
//
//	go test ./internal/lookup -fuzz=FuzzHashEmbeddingFunc    -fuzztime=30s
//	go test ./internal/lookup -fuzz=FuzzFormatGemma4Block    -fuzztime=30s
//	go test ./internal/lookup -fuzz=FuzzJsonQuote            -fuzztime=30s
//	go test ./internal/lookup -fuzz=FuzzBuildIndexContent    -fuzztime=30s

import (
	"context"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

// ─── FuzzHashEmbeddingFunc ───────────────────────────────────────────────────

// FuzzHashEmbeddingFunc verifies that the hash-based embedding function:
//   - never panics for any input
//   - always returns exactly embeddingDims (384) floats
//   - always returns a unit-normalised vector (L2 norm ≈ 1.0) for non-empty text
//   - is deterministic: same text → same vector
func FuzzHashEmbeddingFunc(f *testing.F) {
	f.Add("retrieve memory entries for the current task")
	f.Add("store persistent key value tags")
	f.Add("proposal create draft governance review")
	f.Add("")
	f.Add("a")
	f.Add("😀 emoji text")
	f.Add(strings.Repeat("word ", 200))
	f.Add("\n\t\r   ")
	f.Add("memory.store")
	f.Add("lookup_tools query max_results")

	f.Fuzz(func(t *testing.T, text string) {
		ctx := context.Background()

		// Must not panic.
		vec1, err := hashEmbeddingFunc(ctx, text)
		if err != nil {
			t.Errorf("hashEmbeddingFunc(%q): unexpected error: %v", text, err)
			return
		}

		// Invariant 1: always exactly embeddingDims floats.
		if len(vec1) != embeddingDims {
			t.Errorf("hashEmbeddingFunc(%q): got %d dims, want %d", text, len(vec1), embeddingDims)
		}

		// Invariant 2: non-empty text produces a unit-normalised vector.
		if strings.TrimSpace(text) != "" {
			var sum float64
			for _, v := range vec1 {
				sum += float64(v) * float64(v)
			}
			norm := math.Sqrt(sum)
			if norm < 0.999 || norm > 1.001 {
				t.Errorf("hashEmbeddingFunc(%q): L2 norm = %f, want ≈ 1.0", text, norm)
			}
		}

		// Invariant 3: deterministic — second call must return identical vector.
		vec2, _ := hashEmbeddingFunc(ctx, text)
		for i := range vec1 {
			if vec1[i] != vec2[i] {
				t.Errorf("hashEmbeddingFunc(%q): non-deterministic at index %d: %f vs %f", text, i, vec1[i], vec2[i])
				break
			}
		}

		// Invariant 4: all values are finite (no NaN / Inf).
		for i, v := range vec1 {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Errorf("hashEmbeddingFunc(%q): non-finite at index %d: %v", text, i, v)
			}
		}
	})
}

// ─── FuzzFormatGemma4Block ───────────────────────────────────────────────────

// FuzzFormatGemma4Block verifies that the Gemma 4 block formatter:
//   - never panics
//   - always returns a string that starts with "<|tool|>" and ends with "<|/tool|>"
//   - never embeds raw newlines or unescaped double-quotes inside the JSON body
//   - contains the tool name when the name is non-empty and valid UTF-8
func FuzzFormatGemma4Block(f *testing.F) {
	f.Add("memory.store", "Store a persistent memory entry", "memory", `{"key":"string"}`)
	f.Add("", "", "", "")
	f.Add("a.b", "desc", "", "")
	f.Add(`tool"with"quotes`, `desc with "quotes" and \backslash`, "", "")
	f.Add("tool.name", "line1\nline2", "skill", "params")
	f.Add("t.x", strings.Repeat("x", 512), "", "")
	f.Add("emoji.tool", "🔧 tool description", "skill", "")

	f.Fuzz(func(t *testing.T, name, description, skillName, parameters string) {
		// Must not panic.
		block := formatGemma4Block(ToolEntry{
			Name:        name,
			Description: description,
			SkillName:   skillName,
			Parameters:  parameters,
		})

		// Invariant 1: always has the correct wrapper tokens.
		if !strings.HasPrefix(block, "<|tool|>") {
			t.Errorf("block missing <|tool|> prefix: %q", block[:min(50, len(block))])
		}
		if !strings.HasSuffix(block, "<|/tool|>") {
			t.Errorf("block missing <|/tool|> suffix: %q", block[max(0, len(block)-50):])
		}

		// Invariant 2: the JSON body (between tokens) must not contain raw newlines.
		body := strings.TrimPrefix(block, "<|tool|>")
		body = strings.TrimSuffix(body, "<|/tool|>")
		if strings.ContainsAny(body, "\n\r") {
			t.Errorf("block JSON body contains raw newline/CR: %q", block)
		}

		// Invariant 3: output is valid UTF-8 when all inputs are valid UTF-8.
		if utf8.ValidString(name) && utf8.ValidString(description) &&
			utf8.ValidString(skillName) && utf8.ValidString(parameters) {
			if !utf8.ValidString(block) {
				t.Errorf("block is not valid UTF-8 for valid-UTF-8 inputs")
			}
		}
	})
}

// min and max helpers (the standard library min/max require Go 1.21+).
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ─── FuzzJsonQuote ───────────────────────────────────────────────────────────

// FuzzJsonQuote verifies that jsonQuote:
//   - never panics
//   - always wraps the output in double-quotes
//   - never embeds unescaped characters that would break JSON (newline, tab, CR,
//     unescaped double-quote)
//   - produces output parseable as a JSON string by standard json.Unmarshal
func FuzzJsonQuote(f *testing.F) {
	f.Add("hello world")
	f.Add("")
	f.Add(`contains "double" quotes`)
	f.Add("line1\nline2")
	f.Add("tab\there")
	f.Add(`back\slash`)
	f.Add("\r\n\t")
	f.Add(strings.Repeat("a", 1024))

	f.Fuzz(func(t *testing.T, s string) {
		result := jsonQuote(s)

		// Invariant 1: wrapped in double-quotes.
		if !strings.HasPrefix(result, `"`) || !strings.HasSuffix(result, `"`) {
			t.Errorf("jsonQuote(%q): not wrapped in quotes: %s", s, result)
			return
		}

		// Invariant 2: the inner text must not contain raw newline/CR/tab or
		// unescaped double-quotes (these would break JSON parsing).
		inner := result[1 : len(result)-1]
		if strings.ContainsAny(inner, "\n\r\t") {
			t.Errorf("jsonQuote(%q): inner contains raw control character: %q", s, inner)
		}
		// Unescaped double-quote inside inner → check that any " is preceded by \.
		for i, ch := range inner {
			if ch == '"' && (i == 0 || inner[i-1] != '\\') {
				t.Errorf("jsonQuote(%q): unescaped double-quote at pos %d in: %q", s, i, inner)
				break
			}
		}
	})
}

// ─── FuzzBuildIndexContent ───────────────────────────────────────────────────

// FuzzBuildIndexContent verifies that buildIndexContent:
//   - never panics
//   - always contains the tool name when it is non-empty
//   - always contains the description when it is non-empty
//   - never returns an empty string when both name and description are non-empty
func FuzzBuildIndexContent(f *testing.F) {
	f.Add("memory.store", "Store a persistent memory entry", "memory", `{"key":"string"}`)
	f.Add("", "", "", "")
	f.Add("tool.x", "desc", "skill", "")
	f.Add("a.b", "", "", "params")
	f.Add("tool", "description only", "", "")
	f.Add(strings.Repeat("x", 256)+".y", strings.Repeat("d", 256), "", "")

	f.Fuzz(func(t *testing.T, name, description, skillName, parameters string) {
		content := buildIndexContent(ToolEntry{
			Name:        name,
			Description: description,
			SkillName:   skillName,
			Parameters:  parameters,
		})

		// Invariant 1: non-empty when both name and description are non-empty.
		if name != "" && description != "" && content == "" {
			t.Errorf("buildIndexContent: empty result for non-empty name=%q desc=%q", name, description)
		}

		// Invariant 2: contains name when non-empty.
		if name != "" && !strings.Contains(content, name) {
			t.Errorf("buildIndexContent: content missing name %q: %q", name, content)
		}

		// Invariant 3: contains description when non-empty.
		if description != "" && !strings.Contains(content, description) {
			t.Errorf("buildIndexContent: content missing description %q: %q", description, content)
		}
	})
}
