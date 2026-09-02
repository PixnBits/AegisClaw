package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"AegisClaw/internal/storegit"
)

// git-remote-hub speaks git's remote-helper protocol and relays
// git-upload-pack / git-receive-pack to Store over a private unix socket.
// The clone URL (hub::vsock/<tenant>/<repo>) still looks like Hub/vsock;
// the socket path is never put in the URL.
func main() {
	url := ""
	switch {
	case len(os.Args) >= 3:
		url = os.Args[2]
	case len(os.Args) >= 2:
		url = os.Args[1]
	}
	tenant, repo, ok := storegit.ParseURL(url)
	if !ok {
		fmt.Fprintf(os.Stderr, "git-remote-hub: cannot parse remote %q\n", url)
		os.Exit(1)
	}

	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadString('\n')
		if err != nil && len(strings.TrimSpace(line)) == 0 {
			if err == io.EOF {
				return
			}
			fmt.Fprintf(os.Stderr, "git-remote-hub: read: %v\n", err)
			os.Exit(1)
		}
		cmd := strings.TrimRight(line, "\r\n")
		switch {
		case cmd == "capabilities":
			fmt.Print("connect\n\n")
		case strings.HasPrefix(cmd, "option "):
			fmt.Print("unsupported\n")
		case strings.HasPrefix(cmd, "connect "):
			service := strings.TrimSpace(strings.TrimPrefix(cmd, "connect "))
			if err := connect(service, tenant, repo, in); err != nil {
				fmt.Fprintf(os.Stderr, "git-remote-hub: %v\n", err)
				os.Exit(1)
			}
			return
		case cmd == "":
			return
		default:
			fmt.Fprintf(os.Stderr, "git-remote-hub: unknown command %q\n", cmd)
			os.Exit(1)
		}
	}
}

func connect(service, tenant, repo string, buffered *bufio.Reader) error {
	conn, err := dialStore()
	if err != nil {
		return err
	}
	defer conn.Close()

	proto := os.Getenv("GIT_PROTOCOL")
	header := service + " " + tenant + " " + repo
	if proto != "" {
		header += " " + proto
	}
	if _, err := fmt.Fprintf(conn, "%s\n", header); err != nil {
		return err
	}

	// Blank line tells git(1) the connection succeeded; then stdin/stdout
	// is the raw git pack protocol.
	if _, err := os.Stdout.Write([]byte("\n")); err != nil {
		return err
	}
	_ = os.Stdout.Sync()

	errCh := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(conn, io.MultiReader(buffered, os.Stdin))
		if uc, ok := conn.(*net.UnixConn); ok {
			_ = uc.CloseWrite()
		}
		errCh <- copyErr
	}()
	go func() {
		_, copyErr := io.Copy(os.Stdout, conn)
		errCh <- copyErr
	}()
	err1 := <-errCh
	err2 := <-errCh
	if err1 != nil {
		return err1
	}
	return err2
}

func dialStore() (net.Conn, error) {
	var last error
	for _, p := range storegit.SocketCandidates("") {
		c, err := net.Dial("unix", p)
		if err == nil {
			return c, nil
		}
		last = err
	}
	if last == nil {
		return nil, fmt.Errorf("no store git socket candidates")
	}
	return nil, fmt.Errorf("dial store git socket: %w", last)
}
