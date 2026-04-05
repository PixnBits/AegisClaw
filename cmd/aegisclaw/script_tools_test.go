package main

import (
	"encoding/json"
	"testing"
)

func TestParseRunScriptParams_Valid(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{
		"language":   "python",
		"code":       "print('ok')",
		"args":       []string{"a", "b"},
		"timeout_ms": 3000,
	})
	p, err := parseRunScriptParams(string(args))
	if err == nil {
		if p.Language != "python" || p.Code == "" {
			t.Fatalf("unexpected parsed params: %#v", p)
		}
		return
	}
	t.Fatalf("unexpected parse error: %v", err)
}

func TestParseRunScriptParams_InvalidLanguage(t *testing.T) {
	args := `{"language":"ruby","code":"puts 1"}`
	_, err := parseRunScriptParams(args)
	if err == nil {
		t.Fatal("expected unsupported language error")
	}
}

func TestSupportedScriptLanguages(t *testing.T) {
	langs := supportedScriptLanguages()
	if len(langs) == 0 {
		t.Fatal("expected non-empty language list")
	}
	want := map[string]bool{"python": true, "javascript": true, "bash": true, "sh": true}
	for _, l := range langs {
		delete(want, l)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected languages: %#v", want)
	}
}

func TestScriptRuntimeCommandsUseAbsolutePaths(t *testing.T) {
	want := map[string]string{
		"python":     "/usr/bin/python3",
		"javascript": "/usr/bin/node",
		"bash":       "/bin/bash",
		"sh":         "/bin/sh",
	}
	for lang, expected := range want {
		got := scriptRuntimeCommands[lang]
		if len(got) == 0 || got[0] != expected {
			t.Fatalf("runtime command for %s = %#v, want first element %q", lang, got, expected)
		}
	}
}

func TestBuildRunScriptExecRequest(t *testing.T) {
	params := &runScriptParams{
		Language:  "python",
		Code:      "print('ok')",
		Args:      []string{"a", "b"},
		TimeoutMS: 3200,
	}
	req, err := buildRunScriptExecRequest(params, scriptRuntimeCommands["python"])
	if err != nil {
		t.Fatalf("buildRunScriptExecRequest returned error: %v", err)
	}
	if req["type"] != "exec" {
		t.Fatalf("request type = %v, want exec", req["type"])
	}
	payload, ok := req["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload type = %T, want map[string]interface{}", req["payload"])
	}
	if payload["command"] != "/usr/bin/python3" {
		t.Fatalf("command = %v, want /usr/bin/python3", payload["command"])
	}
	args, ok := payload["args"].([]string)
	if !ok {
		t.Fatalf("args type = %T, want []string", payload["args"])
	}
	wantArgs := []string{"-c", "print('ok')", "a", "b"}
	if len(args) != len(wantArgs) {
		t.Fatalf("args length = %d, want %d (%#v)", len(args), len(wantArgs), args)
	}
	for i := range wantArgs {
		if args[i] != wantArgs[i] {
			t.Fatalf("args[%d] = %q, want %q", i, args[i], wantArgs[i])
		}
	}
	if payload["dir"] != "/workspace" {
		t.Fatalf("dir = %v, want /workspace", payload["dir"])
	}
	if payload["timeout_secs"] != 4 {
		t.Fatalf("timeout_secs = %v, want 4", payload["timeout_secs"])
	}
}

func TestBuildRunScriptExecRequest_ClampsTimeout(t *testing.T) {
	tests := []struct {
		name      string
		timeoutMS int
		wantSecs  int
	}{
		{name: "default", timeoutMS: 0, wantSecs: 5},
		{name: "minimum", timeoutMS: 1, wantSecs: 1},
		{name: "rounded up", timeoutMS: 1001, wantSecs: 2},
		{name: "maximum", timeoutMS: 120000, wantSecs: 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildRunScriptExecRequest(&runScriptParams{Language: "sh", Code: "echo ok", TimeoutMS: tt.timeoutMS}, scriptRuntimeCommands["sh"])
			if err != nil {
				t.Fatalf("buildRunScriptExecRequest returned error: %v", err)
			}
			payload := req["payload"].(map[string]interface{})
			if payload["timeout_secs"] != tt.wantSecs {
				t.Fatalf("timeout_secs = %v, want %d", payload["timeout_secs"], tt.wantSecs)
			}
		})
	}
}
