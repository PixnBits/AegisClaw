package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// git-remote-hub speaks git's remote-helper protocol and relays
// git-upload-pack / git-receive-pack through Hub (AEGIS_HUB_SOCKET).
// The clone URL (hub::vsock/<tenant>/<repo>) still looks like Hub/vsock.
// Caller identity is Hub's, not the URL tenant and not a sibling git.sock header.
func main() {
	url := ""
	switch {
	case len(os.Args) >= 3:
		url = os.Args[2]
	case len(os.Args) >= 2:
		url = os.Args[1]
	}
	if strings.TrimSpace(url) == "" {
		fmt.Fprintf(os.Stderr, "git-remote-hub: missing remote url\n")
		os.Exit(1)
	}
	hubSock := os.Getenv("AEGIS_HUB_SOCKET")
	if hubSock == "" {
		fmt.Fprintf(os.Stderr, "git-remote-hub: AEGIS_HUB_SOCKET required (no git.sock)\n")
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
			if err := connectHub(hubSock, service, url, in); err != nil {
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

func connectHub(hubSock, service, url string, buffered *bufio.Reader) error {
	conn, err := net.Dial("unix", hubSock)
	if err != nil {
		return fmt.Errorf("dial hub: %w", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "git-connect %s %s\n", service, url); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	reply, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("hub reply: %w", err)
	}
	reply = strings.TrimSpace(reply)
	if reply != "ok" {
		return fmt.Errorf("%s", reply)
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
		_, copyErr := io.Copy(os.Stdout, br)
		errCh <- copyErr
	}()
	err1 := <-errCh
	err2 := <-errCh
	if err1 != nil {
		return err1
	}
	return err2
}
