package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	listenSocket = "/run/seedclaw.sock" // must match what seedclaw mounts
)

func main() {
	// Remove old socket if exists (previous run)
	if err := os.Remove(listenSocket); err != nil && !os.IsNotExist(err) {
		log.Fatalf("cannot remove old socket: %v", err)
	}

	// Create unix socket
	ln, err := net.Listen("unix", listenSocket)
	if err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	// Make socket world-readable/writable so host can connect
	if err := os.Chmod(listenSocket, 0666); err != nil {
		log.Fatalf("chmod failed: %v", err)
	}

	log.Printf("Message-Hub listening on unix socket: %s", listenSocket)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept error: %v", err)
			continue
		}

		log.Printf("New connection from seedclaw")

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("read error: %v", err)
			}
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		log.Printf("[seedclaw → hub] %q", line)

		// MVP: just echo back with prefix (later: real routing table)
		reply := fmt.Sprintf("hub received: %s (at %s)", line, time.Now().Format(time.RFC3339))

		_, err = writer.WriteString(reply + "\n")
		if err != nil {
			log.Printf("write error: %v", err)
			return
		}
		writer.Flush()
	}
}
