package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	defaultScriptTimeout = 3 * time.Second
	maxScriptTimeout     = 10 * time.Second
	maxScriptSize        = 32 * 1024
	maxScriptArgs        = 16
	maxScriptArgLen      = 256
	maxToolOutputBytes   = 4096
)

var scriptRuntimeCommands = map[string][]string{
	"python":     {"python3", "-c"},
	"javascript": {"node", "-e"},
	"bash":       {"bash", "-c"},
	"sh":         {"sh", "-c"},
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

func scriptTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		return defaultScriptTimeout
	}
	d := time.Duration(timeoutMS) * time.Millisecond
	if d > maxScriptTimeout {
		return maxScriptTimeout
	}
	return d
}

func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n...[truncated]"
}

func runScript(ctx context.Context, params *runScriptParams) (string, error) {
	cmdSpec := scriptRuntimeCommands[params.Language]
	timeout := scriptTimeout(params.TimeoutMS)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := []string{cmdSpec[1], params.Code}
	argv = append(argv, params.Args...)
	cmd := exec.CommandContext(runCtx, cmdSpec[0], argv...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	duration := time.Since(start)

	outStr := truncateOutput(stdout.String(), maxToolOutputBytes)
	errStr := truncateOutput(stderr.String(), maxToolOutputBytes)

	result := map[string]interface{}{
		"language":    params.Language,
		"runtime":     cmdSpec[0],
		"duration_ms": duration.Milliseconds(),
		"stdout":      outStr,
		"stderr":      errStr,
	}
	if runCtx.Err() == context.DeadlineExceeded {
		result["timed_out"] = true
	}

	respJSON, _ := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return string(respJSON), fmt.Errorf("script execution failed: %w", err)
	}
	return string(respJSON), nil
}

func supportedScriptLanguages() []string {
	langs := make([]string, 0, len(scriptRuntimeCommands))
	for lang := range scriptRuntimeCommands {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return langs
}
