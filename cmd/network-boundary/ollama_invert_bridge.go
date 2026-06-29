package main

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/ollamametrics"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
)

var (
	ollamaBridgeMu   sync.Mutex
	ollamaBridgeConn net.Conn
	ollamaBridgeEnc  *json.Encoder
	ollamaBridgeDec  *json.Decoder
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

// startOllamaInvertBridge listens for the host Ollama bridge (inverted vsock path).
func startOllamaInvertBridge() {
	go func() {
		ln, err := vsock.Listen(hubclient.OllamaBridgeGuestPort, nil)
		if err != nil {
			fmt.Printf("network-boundary: ollama invert bridge listen failed: %v\n", err)
			return
		}
		fmt.Printf("network-boundary: ollama invert bridge listening on vsock :%d (host dials for llm.call)\n",
			hubclient.OllamaBridgeGuestPort)
		for {
			conn, err := ln.Accept()
			if err != nil {
				continue
			}
			ollamaBridgeMu.Lock()
			if ollamaBridgeConn != nil {
				_ = ollamaBridgeConn.Close()
			}
			ollamaBridgeConn = conn
			ollamaBridgeEnc = json.NewEncoder(conn)
			ollamaBridgeDec = json.NewDecoder(conn)
			ollamaBridgeMu.Unlock()
			fmt.Println("network-boundary: host ollama bridge connected")
		}
	}()
}

func callOllamaViaInvertBridge(model, prompt, endpoint string) (string, error) {
	ollamaBridgeMu.Lock()
	enc := ollamaBridgeEnc
	dec := ollamaBridgeDec
	conn := ollamaBridgeConn
	ollamaBridgeMu.Unlock()
	if conn == nil || enc == nil || dec == nil {
		return "", fmt.Errorf("host ollama bridge not connected (waiting for host dial on vsock :%d)",
			hubclient.OllamaBridgeGuestPort)
	}
	ollamaBridgeMu.Lock()
	defer ollamaBridgeMu.Unlock()
	if err := enc.Encode(ollamaBridgeReq{Model: model, Prompt: prompt, Endpoint: endpoint}); err != nil {
		return "", fmt.Errorf("ollama bridge encode: %w", err)
	}
	var resp ollamaBridgeResp
	if err := dec.Decode(&resp); err != nil {
		return "", fmt.Errorf("ollama bridge decode: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("%s", resp.Error)
	}
	if resp.Response == "" {
		return "", fmt.Errorf("empty ollama bridge response")
	}
	// Invert-bridge response path: capture raw + log metrics (calls the pure helper).
	if model, counts, err := ollamametrics.ParseGenerateMetrics([]byte(resp.Response)); err == nil {
		ollamametrics.LogLLMMetrics(model, len(prompt), counts)
	}
	return ollamametrics.ExtractResponseText([]byte(resp.Response)), nil
}

func callOllamaGenerate(model, prompt, endpoint string) (string, error) {
	if bootargs.UseHubVsock() {
		return callOllamaViaInvertBridge(model, prompt, endpoint)
	}
	return callOllamaDirectHTTP(model, prompt, endpoint)
}
