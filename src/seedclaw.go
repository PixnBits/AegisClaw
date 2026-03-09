package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

const (
	socketDir   = "./shared/sockets/control"
	socketPath  = socketDir + "/seedclaw.sock"
	composeFile = "compose.yaml"
)

var controlListener net.Listener

// GID that message-hub will run as inside its container. Recommend creating
// a matching group on the host with this GID and chowning the socket dir to it.
const socketGroupGID = 1500

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

	// Ensure the socket directory is accessible by container processes that
	// may be running as different UIDs. Make it world-traversable so the
	// container can reach the socket regardless of host group configuration.
	if err := os.Chmod(socketDir, 0777); err != nil {
		log.Printf("Warning: chmod failed on socket dir: %v", err)
	}

	// 3. Create and listen on the socket BEFORE starting containers
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}
	// Keep the listener open for the lifetime of the program so the
	// message-hub can connect to the socket. Do not close it here.
	controlListener = ln

	// Make the socket world-writable so containers that cannot change
	// ownership (rootless/uid-mapped environments) can still connect.
	if err := os.Chmod(socketPath, 0666); err != nil {
		log.Printf("Warning: chmod failed on socket: %v", err)
	}
	// Log current socket file mode for debugging
	if fi, err := os.Stat(socketPath); err == nil {
		log.Printf("socket stat: %v", fi.Mode())
	} else {
		log.Printf("socket stat failed: %v", err)
	}

	// 4. Start a local TCP->Unix proxy so containers can connect to the
	// host control socket without bind-mounting it. The proxy listens on
	// loopback and containers reach the host via Docker's host-gateway.
	var proxyWg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		proxyWg.Wait()
	}()

	proxyWg.Add(1)
	go func() {
		defer proxyWg.Done()
		// Bind to all interfaces so containers can reach the proxy via
		// Docker's host-gateway mapping.
		if err := runProxy(ctx, "0.0.0.0:50023", socketPath); err != nil {
			log.Printf("control proxy stopped: %v", err)
		}
	}()

	// Give proxy a moment to start
	time.Sleep(200 * time.Millisecond)

	// 5. Start compose now that the socket file exists and proxy is listening
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

	return nil
}

func runChat() error {
	fmt.Println("Chat mode ready. Type messages (empty line or Ctrl+D to exit):")

	// Accept a single connection from the message-hub on the pre-existing listener
	if controlListener == nil {
		return fmt.Errorf("control listener not initialized")
	}
	conn, err := controlListener.Accept()
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

// runProxy forwards TCP connections on listenAddr to the unix domain socket
// at targetSocket. It runs until the provided context is cancelled.
func runProxy(ctx context.Context, listenAddr, targetSocket string) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("proxy listen %s failed: %w", listenAddr, err)
	}
	defer l.Close()

	var tempDelay time.Duration
	for {
		// Check context
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		l.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))
		conn, err := l.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return fmt.Errorf("proxy accept error: %w", err)
		}

		go func(c net.Conn) {
			defer c.Close()
			// Dial the unix socket on the host
			uc, err := net.Dial("unix", targetSocket)
			if err != nil {
				log.Printf("proxy: dial unix %s failed: %v", targetSocket, err)
				return
			}
			defer uc.Close()

			// Bi-directional copy
			done := make(chan struct{})
			go func() {
				io.Copy(uc, c)
				uc.(*net.UnixConn).CloseWrite()
				close(done)
			}()
			io.Copy(c, uc)
			c.(*net.TCPConn).CloseWrite()
			<-done
		}(conn)

		// backoff (not expected to trigger frequently)
		if tempDelay == 0 {
			tempDelay = 5 * time.Millisecond
		} else {
			tempDelay *= 2
		}
		if tempDelay > 1*time.Second {
			tempDelay = 1 * time.Second
		}
		time.Sleep(tempDelay)
	}
}
