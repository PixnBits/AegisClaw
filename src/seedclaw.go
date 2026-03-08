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
	socketDir      = "./shared/sockets/control"
	socketPath     = socketDir + "/seedclaw.sock"
	composeFile    = "compose.yaml"
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
	// 1. Ensure shared directories exist
	if err := ensureSharedDirs(); err != nil {
		return err
	}

	// 2. Clean up any previous socket state
	if err := cleanupSocket(); err != nil {
		return err
	}

	// 3. Create and listen on the socket BEFORE starting containers
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}
	defer ln.Close() // will close after accept in runChat, but we listen early

	// Make socket accessible to container (non-root user)
	if err := os.Chmod(socketPath, 0666); err != nil {
		log.Printf("Warning: chmod failed on socket: %v", err)
	}

	// 4. Start compose now that the socket file exists
	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		return fmt.Errorf("compose.yaml not found in current directory")
	}

	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose up -d failed: %w", err)
	}

	fmt.Printf("Core services starting. Control socket created at %s\n", socketPath)
	time.Sleep(1500 * time.Millisecond) // brief wait for container startup

	return nil
}

func ensureSharedDirs() error {
	dirs := []string{
		"./shared/sockets/control",
		"./shared/sources",
		"./shared/builds",
		"./shared/outputs",
		"./shared/logs",
		"./shared/audit",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("cannot create shared directory %s: %w", d, err)
		}
	}
	return nil
}

func cleanupSocket() error {
	// Remove socket file if it exists (not directory)
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot clean up old socket %s: %w\n\n"+
			"Try manually:\n  sudo rm -f %s\nthen retry",
			socketPath, err, socketPath)
	}

	// If someone left a directory there (old bug), clean it
	fi, err := os.Stat(socketDir)
	if err == nil && fi.IsDir() {
		if err := os.RemoveAll(socketDir); err != nil {
			return fmt.Errorf("cannot remove bad directory %s: %w\n\n"+
				"Try manually:\n  sudo rm -rf %s\nthen retry",
				socketDir, err, socketDir)
		}
		log.Printf("Removed stale directory %s", socketDir)
	}

	return nil
}

func runChat() error {
	fmt.Println("Chat mode ready. Type messages (empty line or Ctrl+D to exit):")

	// Re-listen in case cleanup happened after initial listen
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("cannot listen in chat mode: %w", err)
	}
	defer ln.Close()

	conn, err := ln.Accept()
	if err != nil {
		return fmt.Errorf("accept failed: %w", err)
	}
	fmt.Println("Message-hub connected!")

	defer conn.Close()

	// Forward replies from hub to stdout
	go func() {
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			fmt.Println("[hub]", scanner.Text())
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

	fmt.Println("Exiting chat.")
	return nil
}
