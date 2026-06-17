package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"AegisClaw/internal/config"
	"AegisClaw/internal/sandbox"
	"AegisClaw/internal/transport/hubclient"

	"github.com/sirupsen/logrus"
)

type ollamaBridgeReq struct {
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Endpoint string `json:"endpoint"`
}

type ollamaBridgeResp struct {
	Response string `json:"response,omitempty"`
	Error    string `json:"error,omitempty"`
}

// startOllamaGuestBridge connects the host to the network-boundary guest's inverted
// Ollama bridge (vsock :9102). The guest sends llm.call payloads; we proxy to host Ollama.
func startOllamaGuestBridge(vmID string) {
	if cfg == nil || cfg.SandboxType != config.Firecracker || vmID == "" {
		return
	}
	go func() {
		runOllamaGuestBridge(cfg.StateDir, vmID)
	}()
}

func runOllamaGuestBridge(stateDir, vmID string) {
	udsPath := sandbox.FirecrackerVsockUDSPath(stateDir, vmID)
	port := uint32(hubclient.OllamaBridgeGuestPort)
	backend := ollamaBackendURL()
	client := &http.Client{Timeout: 180 * time.Second}

	for {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		guestConn, err := dialFirecrackerVsockWithRetry(ctx, udsPath, port, 120, 500*time.Millisecond)
		cancel()
		if err != nil {
			logrus.Debugf("ollama guest bridge %s: guest listener not ready: %v", vmID, err)
			time.Sleep(200 * time.Millisecond)
			continue
		}

		logrus.Infof("ollama guest bridge connected: %s (vsock :%d) -> %s", vmID, port, backend)
		dec := json.NewDecoder(guestConn)
		enc := json.NewEncoder(guestConn)
		for {
			var req ollamaBridgeReq
			if err := dec.Decode(&req); err != nil {
				if err != io.EOF {
					logrus.Debugf("ollama guest bridge %s: decode: %v", vmID, err)
				}
				break
			}
			resp := ollamaBridgeResp{}
			text, err := callHostOllama(client, backend, req)
			if err != nil {
				resp.Error = err.Error()
			} else {
				resp.Response = text
			}
			if err := enc.Encode(resp); err != nil {
				logrus.Debugf("ollama guest bridge %s: encode: %v", vmID, err)
				break
			}
		}
		_ = guestConn.Close()
		logrus.Warnf("ollama guest bridge %s disconnected; reconnecting", vmID)
		time.Sleep(300 * time.Millisecond)
	}
}

func ollamaBackendURL() string {
	host := strings.TrimSpace(os.Getenv("AEGIS_OLLAMA_BACKEND_HOST"))
	if host == "" {
		host = "127.0.0.1:11434"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

func callHostOllama(client *http.Client, backend string, req ollamaBridgeReq) (string, error) {
	endpoint := strings.TrimSpace(req.Endpoint)
	if endpoint == "" {
		endpoint = "/api/generate"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	target, err := url.Parse(backend + endpoint)
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model":  req.Model,
		"prompt": req.Prompt,
		"stream": false,
	})
	httpReq, err := http.NewRequest(http.MethodPost, target.String(), strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
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
