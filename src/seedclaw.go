package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"time"
)

const (
	socketPath     = "/tmp/seedclaw.sock" // will be mounted into message-hub
	composeFile    = "compose.yaml"
	defaultTimeout = 15 * time.Second
)

func main() {
	start := flag.Bool("start", false, "Start core services and enter chat mode")
	flag.Parse()

	if *start {
		if err := startCoreServices(); err != nil {
			log.Fatalf("Failed to start core services: %v", err)
		}
	}

	if err := runChat(); err != nil {
		log.Fatalf("Chat loop failed: %v", err)
	}
}

func startCoreServices() error {
	// Remove old socket if exists
	_ = os.Remove(socketPath)

	// Make sure compose.yaml exists (for MVP we assume it is already in repo)
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose.yaml not found in current directory")
	}

	// docker compose up -d (only the core services should start)
	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	// Wait a bit for message-hub to create/listen on the socket
	time.Sleep(2 * time.Second)

	// Check socket exists
	if _, err := os.Stat(socketPath); err != nil {
		return fmt.Errorf("unix socket %s not created by message-hub: %w", socketPath, err)
	}

	fmt.Println("Core services started. Unix socket ready:", socketPath)
	return nil
}

func runChat() error {
	fmt.Println("SeedClaw chat mode. Type messages below (empty line or Ctrl+D to exit)")

	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot connect to message-hub: %w", err)
	}
	defer conn.Close()

	go func() {
		// Forward replies from hub → stdout
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Println("[hub] " + scanner.Text())
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			log.Printf("read from hub failed: %v", err)
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			break
		}

		_, err := fmt.Fprintf(conn, "%s\n", line)
		if err != nil {
			return fmt.Errorf("write to hub failed: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	fmt.Println("Exiting chat.")
	return nil
}
