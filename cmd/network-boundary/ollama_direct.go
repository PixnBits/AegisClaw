package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func callOllamaDirectHTTP(model, prompt, endpoint string) (string, error) {
	host := ollamaBackendHost()
	target := "http://" + host + endpoint
	ollamaReq := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	bodyBytes, _ := json.Marshal(ollamaReq)
	httpReq, err := http.NewRequest(http.MethodPost, target, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("failed to build ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 180 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()
	respBytes, _ := io.ReadAll(httpResp.Body)
	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama status %d: %s", httpResp.StatusCode, string(respBytes))
	}
	// Return full raw JSON body so caller (boundary) can extract usage fields (prompt_eval_count, eval_count, etc.)
	// + text. Extraction of inner "response" happens at llm.call site for compat.
	return string(respBytes), nil
}
