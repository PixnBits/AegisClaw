package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

// CONTROL_ADDR may be set by compose to point at the host proxy. Default
// uses Docker's host-gateway hostname and port 50023.
var controlAddr = getenv("CONTROL_ADDR", "host.docker.internal:50023")

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func main() {
	log.Printf("Message-Hub starting — connecting to %s", controlAddr)

	var conn net.Conn
	var err error

	// Retry until host proxy accepts
	for i := 0; i < 60; i++ { // ~30 seconds max
		conn, err = net.Dial("tcp", controlAddr)
		if err == nil {
			break
		}
		log.Printf("Waiting for control proxy (%d/60): %v", i+1, err)
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		log.Fatalf("Failed to connect to host control proxy after retries: %v", err)
	}
	defer conn.Close()

	log.Println("Successfully connected to seedclaw control proxy!")

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

		// MVP echo (later: proper routing table)
		reply := fmt.Sprintf("hub received: %s (at %s)", line, time.Now().Format(time.RFC3339))

		_, err = fmt.Fprintln(writer, reply)
		if err != nil {
			log.Printf("write error: %v", err)
			return
		}
		writer.Flush()
	}
}
