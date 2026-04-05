package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
)

const (
	maxScriptSize   = 32 * 1024
	maxScriptArgs   = 16
	maxScriptArgLen = 256
)

var scriptRuntimeCommands = map[string][]string{
	"python":     {"/usr/bin/python3", "-c"},
	"javascript": {"/usr/bin/node", "-e"},
	"bash":       {"/bin/bash", "-c"},
	"sh":         {"/bin/sh", "-c"},
}

type runScriptParams struct {
	Language  string   `json:"language"`
	Code      string   `json:"code"`
	Args      []string `json:"args"`
	TimeoutMS int      `json:"timeout_ms"`
}

func parseRunScriptParams(args string) (*runScriptParams, error) {
	var p runScriptParams
	if err := json.Unmarshal([]byte(args), &p); err != nil {
		return nil, fmt.Errorf("invalid run_script args: %w", err)
	}
	p.Language = strings.ToLower(strings.TrimSpace(p.Language))
	if p.Language == "" {
		return nil, fmt.Errorf("run_script requires 'language'")
	}
	if _, ok := scriptRuntimeCommands[p.Language]; !ok {
		return nil, fmt.Errorf("unsupported script language %q", p.Language)
	}
	if strings.TrimSpace(p.Code) == "" {
		return nil, fmt.Errorf("run_script requires non-empty 'code'")
	}
	if len(p.Code) > maxScriptSize {
		return nil, fmt.Errorf("script too large (%d bytes, max %d)", len(p.Code), maxScriptSize)
	}
	if len(p.Args) > maxScriptArgs {
		return nil, fmt.Errorf("too many args (%d, max %d)", len(p.Args), maxScriptArgs)
	}
	for i, a := range p.Args {
		if len(a) > maxScriptArgLen {
			return nil, fmt.Errorf("arg[%d] too long (%d, max %d)", i, len(a), maxScriptArgLen)
		}
	}
	return &p, nil
}

func supportedScriptLanguages() []string {
	langs := make([]string, 0, len(scriptRuntimeCommands))
	for lang := range scriptRuntimeCommands {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}

func runScriptInSandbox(ctx context.Context, env *runtimeEnv, params *runScriptParams) (string, error) {
	if env == nil {
		return "", fmt.Errorf("runtime environment is nil")
	}
	if err := ensureDefaultScriptRunnerActive(ctx, env); err != nil {
		return "", fmt.Errorf("ensure built-in script runner: %w", err)
	}

	entry, ok := env.Registry.Get(defaultScriptRunnerSkill)
	if !ok {
		return "", fmt.Errorf("skill %q not found", defaultScriptRunnerSkill)
	}

	runtimeCmd, ok := scriptRuntimeCommands[params.Language]
	if !ok {
		return "", fmt.Errorf("unsupported script language %q", params.Language)
	}
	req, err := buildRunScriptExecRequest(params, runtimeCmd)
	if err != nil {
		return "", err
	}

	raw, err := env.Runtime.SendToVM(ctx, entry.SandboxID, req)
	if err != nil {
		return "", fmt.Errorf("exec in script runner sandbox: %w", err)
	}

	var resp struct {
		Success bool            `json:"success"`
		Error   string          `json:"error,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", fmt.Errorf("parse exec response: %w", err)
	}
	if !resp.Success {
		return "", fmt.Errorf("script execution failed: %s", resp.Error)
	}

	var result struct {
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("parse exec result: %w", err)
	}
	if result.ExitCode != 0 {
		msg := strings.TrimSpace(result.Stderr)
		if msg == "" {
			msg = strings.TrimSpace(result.Stdout)
		}
		if msg == "" {
			msg = fmt.Sprintf("exit code %d", result.ExitCode)
		}
		return "", fmt.Errorf("script exited with %d: %s", result.ExitCode, msg)
	}
	out := strings.TrimSpace(result.Stdout)
	if out == "" {
		out = strings.TrimSpace(result.Stderr)
	}
	if out == "" {
		out = "(no output)"
	}
	return out, nil
}

func buildRunScriptExecRequest(params *runScriptParams, runtimeCmd []string) (map[string]interface{}, error) {
	if params == nil {
		return nil, fmt.Errorf("run script params are required")
	}
	if len(runtimeCmd) < 2 {
		return nil, fmt.Errorf("runtime command is incomplete")
	}

	timeoutSecs := 5
	if params.TimeoutMS > 0 {
		timeoutSecs = (params.TimeoutMS + 999) / 1000
	}
	if timeoutSecs < 1 {
		timeoutSecs = 1
	}
	if timeoutSecs > 60 {
		timeoutSecs = 60
	}

	payload := map[string]interface{}{
		"command":      runtimeCmd[0],
		"args":         append(append([]string{}, runtimeCmd[1:]...), append([]string{params.Code}, params.Args...)...),
		"dir":          "/workspace",
		"timeout_secs": timeoutSecs,
	}

	return map[string]interface{}{
		"id":      uuid.New().String(),
		"type":    "exec",
		"payload": payload,
	}, nil
}
