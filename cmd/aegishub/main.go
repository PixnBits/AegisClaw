package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/transport/hubclient" // for HubVsockPort constant (Phase 1.1c vsock support)
	"github.com/mdlayher/vsock"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var hubSocketPath = "~/.aegis/hub.sock"

var registered = make(map[string]*RegisteredComponent)
var aclRules []ACLRule
var registeredMutex sync.RWMutex
var tempConnCounter int = 0
var tempConnMutex sync.Mutex

// pendingRPC correlates synchronous hub RPC replies (daemon ephemeral → agent/store).
var pendingRPC = struct {
	sync.Mutex
	ch map[string]chan Message
}{ch: make(map[string]chan Message)}

func isEphemeralHubClient(id string) bool {
	return id == "aegis-daemon-temp" ||
		strings.HasPrefix(id, "aegis-daemon-temp-") ||
		strings.HasPrefix(id, "daemon-temp-")
}

func registerPendingRPC(requesterID string) chan Message {
	ch := make(chan Message, 1)
	pendingRPC.Lock()
	pendingRPC.ch[requesterID] = ch
	pendingRPC.Unlock()
	return ch
}

func clearPendingRPC(requesterID string) {
	pendingRPC.Lock()
	delete(pendingRPC.ch, requesterID)
	pendingRPC.Unlock()
}

func deliverPendingRPC(destID string, msg Message) bool {
	pendingRPC.Lock()
	ch, ok := pendingRPC.ch[destID]
	pendingRPC.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- msg:
		return true
	default:
		return false
	}
}

type ComponentEncoders struct {
	Encoder *json.Encoder
	Decoder *json.Decoder
	Mutex   sync.Mutex
}

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

type RegisteredComponent struct {
	ID        string
	PublicKey ed25519.PublicKey
	Encoders  *ComponentEncoders
	Version   string
}

type ACLRule struct {
	Source      string   `yaml:"source"`
	Destination string   `yaml:"destination"`
	Commands    []string `yaml:"commands"`
}

type ACLConfig struct {
	Rules []ACLRule `yaml:"rules"`
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

var aclFilePath string
var lastACLModTime time.Time

func findACLFile() string {
	if p := os.Getenv("AEGIS_ACL_FILE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}

	candidates := []string{
		"config/acls.yaml",
		"./config/acls.yaml",
		filepath.Join(filepath.Dir(os.Args[0]), "config/acls.yaml"),
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "config/acls.yaml"))
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loadACL() {
	path := findACLFile()
	if path == "" {
		log.Printf("No ACL file found, using default deny-all")
		aclRules = nil
		aclFilePath = ""
		return
	}
	aclFilePath = path

	file, err := os.Open(path)
	if err != nil {
		log.Printf("Failed to open ACL %s: %v", path, err)
		return
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var config ACLConfig
	if err := decoder.Decode(&config); err != nil {
		log.Printf("Failed to decode ACL: %v", err)
		return
	}
	aclRules = config.Rules
	if fi, err := os.Stat(path); err == nil {
		lastACLModTime = fi.ModTime()
	}
	log.Printf("Loaded %d ACL rules from %s", len(aclRules), path)
}

func reloadACLIfChanged() {
	if aclFilePath == "" {
		return
	}
	fi, err := os.Stat(aclFilePath)
	if err != nil {
		return
	}
	if fi.ModTime().After(lastACLModTime) {
		log.Printf("ACL file changed, reloading...")
		loadACL() // re-use the loader (it will update modtime)
	}
}

func checkACL(source, dest, cmd string) bool {
	for _, rule := range aclRules {
		if !aclMatch(rule.Source, source) {
			continue
		}
		if !aclMatch(rule.Destination, dest) {
			continue
		}
		for _, c := range rule.Commands {
			if aclMatch(c, cmd) {
				return true
			}
		}
	}
	return false
}

// aclMatch supports exact match, "*" wildcard, and suffix "*" prefix-match (e.g. "memory.*" matches "memory.get_context"; "court-persona-*" matches "court-persona-ciso").
// For commands without trailing *, exact match only (stricter than prior loose HasPrefix).
func aclMatch(pattern, value string) bool {
	if pattern == "*" || pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix)
	}
	return false
}

func verifySignature(msg Message, pubKey ed25519.PublicKey) bool {
	// Create a copy without signature
	msgCopy := msg
	msgCopy.Signature = ""
	data, err := json.Marshal(msgCopy)
	if err != nil {
		return false
	}
	sigBytes, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return false
	}
	return ed25519.Verify(pubKey, data, sigBytes)
}

func startHub(cmd *cobra.Command, args []string) {
	loadACL()

	// Hot-reload support per aegishub.md
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			reloadACLIfChanged()
		}
	}()

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

	// Phase 1.1c: Start vsock listener for real Firecracker guest microVMs (Agent Runtime, Memory VM, etc.).
	// Guests connect via vsock using the well-known port (matches hubclient.HubVsockPort = 9999 and Host CID convention).
	// handleConnection is reused exactly (vsock.Conn implements net.Conn).
	// This satisfies aegishub.md §Handshake Sequence: "MicroVM connects to AegisHub via vsock".
	go startVsockListener(conns)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn, conns)
	}
}

// startVsockListener starts a parallel listener for guest microVMs over vsock.
// Port 9999 is the documented control-plane port for AegisHub (distinct from per-VM egress ports 9xxx).
// On non-Linux or environments without vsock support it logs and returns gracefully (no hard failure).
// References: agent-runtime.md §Communication, security-model.md §Isolation Strategy.
func startVsockListener(conns *sync.Map) {
	port := uint32(hubclient.HubVsockPort) // 9999 — matches hubclient and guest dialing convention
	l, err := vsock.Listen(port, nil)
	if err != nil {
		log.Printf("AegisHub: vsock listen on port %d failed (expected on non-Linux or without /dev/vsock): %v — real Firecracker guests (agent/memory VMs) will fall back to unix socket in dev", port, err)
		return
	}
	defer l.Close()

	fmt.Printf("AegisHub: listening on vsock port %d for guest microVMs (Agent Runtime + Memory VM per Phase 1)\n", port)

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("vsock accept error: %v", err)
			continue
		}
		go handleConnection(conn, conns)
	}
}

func handleConnection(conn net.Conn, conns *sync.Map) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// First message must be register.
	// Note: the readiness probe in startManagedHub does a Dial + immediate Close()
	// (no data) to test if the socket is accepting. That produces a clean EOF here
	// on the first Decode and is expected / harmless (not a real client register
	// failure). We log at debug or only for non-EOF errors to keep startup logs clean.
	var regMsg Message
	if err := decoder.Decode(&regMsg); err != nil {
		es := err.Error()
		if es != "EOF" && es != "unexpected EOF" && err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Printf("Failed to decode register message: %v (remote=%v local=%v)", err, conn.RemoteAddr(), conn.LocalAddr())
		}
		return
	}
	if regMsg.Destination != "hub" || regMsg.Command != "register" {
		log.Printf("First message not register: %+v", regMsg)
		encoder.Encode(map[string]string{"error": "ERR_INVALID_HANDSHAKE"})
		return
	}

	// Parse payload for public key
	payloadMap, ok := regMsg.Payload.(map[string]interface{})
	if !ok {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PAYLOAD"})
		return
	}
	pubKeyStr, ok := payloadMap["public_key"].(string)
	if !ok {
		encoder.Encode(map[string]string{"error": "ERR_MISSING_PUBLIC_KEY"})
		return
	}
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyStr)
	if err != nil {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PUBLIC_KEY"})
		return
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		encoder.Encode(map[string]string{"error": "ERR_INVALID_PUBLIC_KEY"})
		return
	}
	pubKey := ed25519.PublicKey(pubKeyBytes)

	// Extract version from payload if available
	version := "unknown"
	if versionStr, ok := payloadMap["version"].(string); ok {
		version = versionStr
		log.Printf("Hub: Registered component %s with version %s", regMsg.Source, version)
	} else {
		log.Printf("Hub: Registered component %s with no version (payload: %+v)", regMsg.Source, payloadMap)
	}

	// Check if already registered
	registeredMutex.Lock()
	componentID := regMsg.Source

	// For daemon connections: if already registered, use a temporary ID
	if regMsg.Source == "daemon" {
		if _, exists := registered[regMsg.Source]; exists {
			// This is a fresh daemon connection (not the persistent one)
			// Give it a temporary ID
			tempConnMutex.Lock()
			tempConnCounter++
			componentID = fmt.Sprintf("daemon-temp-%d", tempConnCounter)
			tempConnMutex.Unlock()
			log.Printf("Hub: Fresh daemon connection registered as %s (original daemon still at %s)", componentID, regMsg.Source)
		}
	} else {
		// Allow re-registration when a guest hub bridge reconnects or a VM restarts.
		if _, exists := registered[regMsg.Source]; exists {
			log.Printf("Hub: component %s re-registering — replacing previous connection", regMsg.Source)
		}
	}

	encoders := &ComponentEncoders{
		Encoder: encoder,
		Decoder: decoder,
		Mutex:   sync.Mutex{},
	}
	registered[componentID] = &RegisteredComponent{ID: componentID, PublicKey: pubKey, Encoders: encoders, Version: version}
	registeredMutex.Unlock()
	debugLog("hub", fmt.Sprintf("Registered component %s (hub id %s) version %s", regMsg.Source, componentID, version))

	conns.Store(componentID, conn)

	// Cleanup when connection closes
	defer func(id string) {
		registeredMutex.Lock()
		delete(registered, id)
		registeredMutex.Unlock()
		conns.Delete(id)
		debugLog("hub", fmt.Sprintf("Cleaned up registration for %s", id))
	}(componentID)

	// Send ACL rules for this component, including the assigned ID
	response := map[string]interface{}{
		"status":      "registered",
		"assigned_id": componentID, // Send the assigned ID back to the client
		"acls":        aclRules,    // TODO: filter for this component
	}
	encoders.Mutex.Lock()
	encoders.Encoder.Encode(response)
	encoders.Mutex.Unlock()

	if isEphemeralHubClient(componentID) {
		ephemeralHubRPCLoop(componentID, encoders)
		return
	}

	// Now handle normal messages
	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			debugLog("hub", fmt.Sprintf("Decode error: %v", err))
			return
		}

		debugLog("hub", fmt.Sprintf("Received message from %s to %s, command: %s", msg.Source, msg.Destination, msg.Command))

		// Verify signature
		registeredMutex.RLock()
		regComp, exists := registered[msg.Source]
		registeredMutex.RUnlock()
		if !exists {
			debugLog("hub", fmt.Sprintf("Unauthorized source %s", msg.Source))
			encoder.Encode(map[string]string{"error": "ERR_UNAUTHORIZED"})
			log.Printf("Audit: unauthorized source %s", msg.Source)
			continue
		}
		// Signature is now strictly required for all real traffic (per aegishub.md + security model).
		// "dummy" is only for early dev; it is logged and treated as failure in non-dev mode.
		if msg.Signature == "" || msg.Signature == "dummy" {
			if os.Getenv("AEGIS_DEV_MODE") != "1" {
				encoder.Encode(map[string]string{"error": "ERR_SIGNATURE_REQUIRED"})
				log.Printf("Audit: missing or dummy signature from %s (set AEGIS_DEV_MODE=1 to allow during development)", msg.Source)
				continue
			}
			log.Printf("DEV MODE: allowing dummy signature from %s", msg.Source)
		} else if !verifySignature(msg, regComp.PublicKey) {
			encoder.Encode(map[string]string{"error": "ERR_INVALID_SIGNATURE"})
			log.Printf("Audit: invalid signature from %s", msg.Source)
			continue
		}

		// Check ACL (skip for version commands for debugging)
		if msg.Command != "get-version" && !checkACL(msg.Source, msg.Destination, msg.Command) {
			encoder.Encode(map[string]string{"error": "ERR_ACL_VIOLATION"})
			log.Printf("Audit: ACL violation %s -> %s : %s", msg.Source, msg.Destination, msg.Command)
			continue
		}

		if msg.Destination == "hub" {
			if msg.Command == "component.list" {
				debugLog("hub", fmt.Sprintf("Received component.list query from %s", msg.Source))
				// Return list of all registered components with versions
				var components []map[string]string
				registeredMutex.RLock()
				for id, comp := range registered {
					if id != "daemon" { // Don't list the daemon itself
						debugLog("hub", fmt.Sprintf("  Including component %s version %s", id, comp.Version))
						components = append(components, map[string]string{
							"id":      id,
							"version": comp.Version,
						})
					}
				}
				registeredMutex.RUnlock()
				response := map[string]interface{}{
					"components": components,
				}
				debugLog("hub", fmt.Sprintf("Sending component.list response with %d components", len(components)))
				encoder.Encode(response)
			} else if msg.Command == "tool.list" {
				// Forward to store
				storeMsg := msg
				storeMsg.Destination = "store"
				registeredMutex.RLock()
				storeComp, ok := registered["store"]
				registeredMutex.RUnlock()
				if ok && storeComp.Encoders != nil {
					storeComp.Encoders.Mutex.Lock()
					storeComp.Encoders.Encoder.Encode(storeMsg)
					storeComp.Encoders.Mutex.Unlock()
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
				// Handle other hub commands
				response := map[string]interface{}{
					"status": "ok",
					"echo":   msg.Payload,
				}
				encoder.Encode(response)
			}
		} else {
			// One-way replies (agent poll/chat responses) vs synchronous RPC (memory.get_context, llm.call).
			if isOneWayHubReply(msg.Command) {
				forwardReplyToRequester(msg)
				continue
			}
			reply := forwardHubRPC(componentID, msg)
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(reply)
			encoders.Mutex.Unlock()
		}
	}
}

// isOneWayHubReply reports commands that are fire-and-forget replies on the wire (hubclient.Reply),
// not request/response RPC pairs (hubclient.Send).
func isOneWayHubReply(command string) bool {
	if command == "response" || command == "ack" {
		return true
	}
	// Destination component replies like memory.response are inbound to the caller, not outbound from it.
	return false
}

// ephemeralHubRPCLoop serves one-shot daemon hub clients (sendToComponentViaHub).
// It is the only reader on the connection, avoiding races with hubclient.Send.
func ephemeralHubRPCLoop(requesterID string, encoders *ComponentEncoders) {
	for {
		var msg Message
		encoders.Mutex.Lock()
		err := encoders.Decoder.Decode(&msg)
		encoders.Mutex.Unlock()
		if err != nil {
			debugLog("hub", fmt.Sprintf("ephemeral RPC %s decode end: %v", requesterID, err))
			return
		}
		if msg.Command != "get-version" && !checkACL(msg.Source, msg.Destination, msg.Command) {
			reply := Message{Command: "error", Payload: "ERR_ACL_VIOLATION"}
			encoders.Mutex.Lock()
			_ = encoders.Encoder.Encode(reply)
			encoders.Mutex.Unlock()
			continue
		}
		reply := forwardHubRPC(requesterID, msg)
		encoders.Mutex.Lock()
		_ = encoders.Encoder.Encode(reply)
		encoders.Mutex.Unlock()
	}
}

func forwardHubRPC(requesterID string, msg Message) Message {
	registeredMutex.RLock()
	destComponent, exists := registered[msg.Destination]
	registeredMutex.RUnlock()
	if !exists || destComponent.Encoders == nil {
		debugLog("hub", fmt.Sprintf("RPC %s -> %s: ERR_DESTINATION_NOT_FOUND (registered=%d)", msg.Source, msg.Destination, len(registered)))
		return Message{Command: "error", Payload: "ERR_DESTINATION_NOT_FOUND"}
	}

	debugLog("hub", fmt.Sprintf("RPC %s -> %s command %s (awaiting reply)", msg.Source, msg.Destination, msg.Command))
	waitCh := registerPendingRPC(requesterID)
	defer clearPendingRPC(requesterID)

	destComponent.Encoders.Mutex.Lock()
	if err := destComponent.Encoders.Encoder.Encode(msg); err != nil {
		destComponent.Encoders.Mutex.Unlock()
		return Message{Command: "error", Payload: err.Error()}
	}
	destComponent.Encoders.Mutex.Unlock()

	rpcTimeout := 120 * time.Second
	switch msg.Command {
	case "chat.message", "user.turn":
		rpcTimeout = 300 * time.Second
	case "chat.tool_events", "chat.thought_events", "chat.stream_progress":
		rpcTimeout = 8 * time.Second
	}

	select {
	case reply := <-waitCh:
		return reply
	case <-time.After(rpcTimeout):
		return Message{Command: "error", Payload: "ERR_RPC_TIMEOUT"}
	}
}

func forwardReplyToRequester(msg Message) {
	registeredMutex.RLock()
	destComponent, exists := registered[msg.Destination]
	registeredMutex.RUnlock()
	if !exists || destComponent.Encoders == nil {
		return
	}
	if deliverPendingRPC(msg.Destination, msg) {
		// Ephemeral daemon RPC consumed the reply; ack the sender so hubclient.Send
		// decode does not block and steal the next inbound RPC (e.g. chat.message).
		registeredMutex.RLock()
		srcComponent, srcOK := registered[msg.Source]
		registeredMutex.RUnlock()
		if srcOK && srcComponent.Encoders != nil {
			ack := Message{
				Source:      "hub",
				Destination: msg.Source,
				Command:     "ack",
				Payload:     map[string]string{"status": "delivered"},
				Timestamp:   time.Now().Format(time.RFC3339),
			}
			srcComponent.Encoders.Mutex.Lock()
			_ = srcComponent.Encoders.Encoder.Encode(ack)
			srcComponent.Encoders.Mutex.Unlock()
		}
		return
	}
	destComponent.Encoders.Mutex.Lock()
	_ = destComponent.Encoders.Encoder.Encode(msg)
	destComponent.Encoders.Mutex.Unlock()
}

func debugLog(component, msg string) {
	f, _ := os.OpenFile("/tmp/hub-debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if f != nil {
		defer f.Close()
		fmt.Fprintf(f, "[%s][%s] %s\n", component, time.Now().Format("15:04:05.000"), msg)
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
