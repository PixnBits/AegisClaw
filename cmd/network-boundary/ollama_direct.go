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
	text := string(respBytes)
	var ollamaOut map[string]interface{}
	if json.Unmarshal(respBytes, &ollamaOut) == nil {
		if r, ok := ollamaOut["response"].(string); ok && r != "" {
			text = r
		}
	}
	return text, nil
}
