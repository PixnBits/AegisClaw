package main

import (
	"bufio"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
)

// git-remote-hub speaks git's remote-helper protocol and relays
// git-upload-pack / git-receive-pack through Hub.
// Host tests (T1–T13 / testunixgit): AEGIS_HUB_SOCKET unix dial.
// Firecracker guest: vsock to host CID + HubVsockPort when the env is unset.
// Dial is independent of the URL tenant; the git-connect line still carries
// service + url. Identity is the Ed25519 peer on this Hub connection
// (register), not the URL tenant and not a sibling git.sock header.
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

// hubMessage matches cmd/aegishub Message JSON tags/field order so
// json.Marshal of an unsigned copy is the same bytes verifySignature checks.
type hubMessage struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

// hubDialKind is unix when AEGIS_HUB_SOCKET is set (host T1–T13 / testunixgit),
// else vsock (Firecracker guest). Extracted so tests do not need /dev/vsock.
type hubDialKind int

const (
	hubDialUnix hubDialKind = iota
	hubDialVsock
)

type hubDialPlan struct {
	kind      hubDialKind
	unixSock  string
	vsockCID  uint32
	vsockPort uint32
}

func hubDialPlanFromEnv(hubSock string) hubDialPlan {
	if s := strings.TrimSpace(hubSock); s != "" {
		return hubDialPlan{kind: hubDialUnix, unixSock: s}
	}
	return hubDialPlan{
		kind:      hubDialVsock,
		vsockCID:  hubclient.HostCID,
		vsockPort: hubclient.HubVsockPort,
	}
}

func dialHub(hubSock string) (net.Conn, error) {
	plan := hubDialPlanFromEnv(hubSock)
	if plan.kind == hubDialUnix {
		return net.Dial("unix", plan.unixSock)
	}
	return vsock.Dial(plan.vsockCID, plan.vsockPort, nil)
}

func connectHub(hubSock, service, url string, buffered *bufio.Reader) error {
	priv, err := parsePrivKey(os.Getenv("AEGIS_HUB_PRIVKEY"))
	if err != nil {
		return fmt.Errorf("peer identity required: %w", err)
	}
	conn, err := dialHub(hubSock)
	if err != nil {
		return fmt.Errorf("dial hub: %w", err)
	}
	defer conn.Close()

	pub := priv.Public().(ed25519.PublicKey)
	reg := hubMessage{
		Source:      "git-remote-hub",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": base64.StdEncoding.EncodeToString(pub),
			"version":    "git-remote-hub",
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(reg)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}
	reg.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	br := bufio.NewReader(conn)
	if err := json.NewEncoder(conn).Encode(reg); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	raw, err := br.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("register reply: %w", err)
	}
	var reply map[string]interface{}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return fmt.Errorf("register reply %q: %w", raw, err)
	}
	if errStr, _ := reply["error"].(string); errStr != "" {
		return fmt.Errorf("register: %s", errStr)
	}

	if _, err := fmt.Fprintf(conn, "git-connect %s %s\n", service, url); err != nil {
		return err
	}
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("hub reply: %w", err)
	}
	line = strings.TrimSpace(line)
	if line != "ok" {
		return fmt.Errorf("hub git-connect %q", line)
	}

	if _, err := os.Stdout.Write([]byte("\n")); err != nil {
		return err
	}
	_ = os.Stdout.Sync()

	errCh := make(chan error, 2)
	go func() {
		_, copyErr := io.Copy(conn, io.MultiReader(buffered, os.Stdin))
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
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

func parsePrivKey(s string) (ed25519.PrivateKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("AEGIS_HUB_PRIVKEY empty")
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("want %d-byte private key, got %d", ed25519.PrivateKeySize, len(raw))
	}
	return ed25519.PrivateKey(raw), nil
}
