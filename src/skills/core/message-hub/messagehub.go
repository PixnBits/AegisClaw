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

const (
	socketPath = "/run/seedclaw.sock"
)

func main() {
	log.Printf("Message-Hub starting — will connect to %s", socketPath)

	// Wait/retry until host creates the socket
	var conn net.Conn
	var err error
	for i := 0; i < 30; i++ { // ~15 seconds max
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		log.Printf("Waiting for socket... (%d/30) %v", i+1, err)
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		log.Fatalf("Cannot connect to host socket after retries: %v", err)
	}
	defer conn.Close()

	log.Println("Connected to seedclaw!")

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

		reply := fmt.Sprintf("hub received: %s (at %s)", line, time.Now().Format(time.RFC3339))

		_, err = writer.WriteString(reply + "\n")
		if err != nil {
			log.Printf("write error: %v", err)
			return
		}
		writer.Flush()
	}
}
