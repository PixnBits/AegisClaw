package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/cobra"
)

var hubSocketPath = "~/.aegis/hub.sock"

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func startHub(cmd *cobra.Command, args []string) {
	socket := expandPath(hubSocketPath)
	dir := filepath.Dir(socket)
	os.MkdirAll(dir, 0700)
	os.Remove(socket)

	listener, err := net.Listen("unix", socket)
	if err != nil {
		fmt.Printf("Failed to start AegisHub: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Println("AegisHub started. Listening on", socket)

	conns := &sync.Map{}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn, conns)
	}
}

func handleConnection(conn net.Conn, conns *sync.Map) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			log.Printf("Decode error: %v", err)
			return
		}

		// Register the source
		conns.Store(msg.Source, conn)

		if msg.Destination == "hub" {
			if msg.Command == "tool.list" {
				// Forward to store
				storeMsg := msg
				storeMsg.Destination = "store"
				if destConn, ok := conns.Load("store"); ok {
					destEncoder := json.NewEncoder(destConn.(net.Conn))
					destEncoder.Encode(storeMsg)
					// Wait for response from store
					var storeResp Message
					err := decoder.Decode(&storeResp)
					if err != nil {
						errorMsg := map[string]string{"error": "failed to get from store"}
						encoder.Encode(errorMsg)
					} else {
						encoder.Encode(storeResp.Payload)
					}
				} else {
					errorMsg := map[string]string{"error": "store not available"}
					encoder.Encode(errorMsg)
				}
			} else {
				// Handle hub commands
				response := map[string]interface{}{
					"status": "ok",
					"echo":   msg.Payload,
				}
				encoder.Encode(response)
			}
		} else {
			if destConn, ok := conns.Load(msg.Destination); ok {
				destEncoder := json.NewEncoder(destConn.(net.Conn))
				destEncoder.Encode(msg)
			} else {
				errorMsg := map[string]string{"error": "destination not found"}
				encoder.Encode(errorMsg)
			}
		}
	}
}

func main() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocketPath = env
	}
	var rootCmd = &cobra.Command{Use: "aegishub"}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the AegisHub",
		Run:   startHub,
	}

	rootCmd.AddCommand(startCmd)
	rootCmd.Execute()
}