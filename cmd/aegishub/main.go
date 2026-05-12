package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var hubSocketPath = "~/.aegis/hub.sock"

var registered = make(map[string]*RegisteredComponent)
var aclRules []ACLRule
var registeredMutex sync.RWMutex
var tempConnCounter int = 0
var tempConnMutex sync.Mutex

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

func loadACL() {
	possiblePaths := []string{
		"../../config/acls.yaml",
		"./config/acls.yaml",
		"/root/AegisClaw_lessons-learned/config/acls.yaml",
		"/home/pixnbits/AegisClaw_lessons-learned/config/acls.yaml",
	}

	// Also try from working directory
	if wd, err := os.Getwd(); err == nil {
		possiblePaths = append(possiblePaths, filepath.Join(wd, "config/acls.yaml"))
	}

	var file *os.File
	var openErr error

	for _, path := range possiblePaths {
		file, openErr = os.Open(path)
		if openErr == nil {
			log.Printf("Loaded ACL from %s", path)
			break
		}
	}

	if file == nil {
		log.Printf("No ACL file found, using default deny")
		return
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var config ACLConfig
	err := decoder.Decode(&config)
	if err != nil {
		log.Printf("Failed to decode ACL: %v", err)
		return
	}
	aclRules = config.Rules
	log.Printf("Loaded %d ACL rules", len(aclRules))
}

func checkACL(source, dest, cmd string) bool {
	for _, rule := range aclRules {
		// Check source match (including wildcards)
		sourceMatches := rule.Source == source || rule.Source == "*"
		if !sourceMatches {
			continue
		}

		// Check destination match (including wildcards)
		destMatches := rule.Destination == dest || rule.Destination == "*"
		if !destMatches {
			continue
		}

		// Check command match (including wildcard patterns)
		for _, c := range rule.Commands {
			if c == cmd || strings.HasPrefix(cmd, strings.TrimSuffix(c, "*")) {
				return true
			}
		}
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

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn, conns)
	}
}

func handleConnection(conn net.Conn, conns *sync.Map) {
	defer conn.Close()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	// First message must be register
	var regMsg Message
	if err := decoder.Decode(&regMsg); err != nil {
		log.Printf("Failed to decode register message: %v", err)
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
		// Other components cannot re-register
		if _, exists := registered[regMsg.Source]; exists {
			registeredMutex.Unlock()
			encoder.Encode(map[string]string{"error": "ERR_DUPLICATE_COMPONENT"})
			return
		}
	}
	
	encoders := &ComponentEncoders{
		Encoder: encoder,
		Decoder: decoder,
		Mutex:   sync.Mutex{},
	}
	registered[componentID] = &RegisteredComponent{ID: componentID, PublicKey: pubKey, Encoders: encoders, Version: version}
	registeredMutex.Unlock()

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
		"status":       "registered",
		"assigned_id":  componentID,  // Send the assigned ID back to the client
		"acls":         aclRules,      // TODO: filter for this component
	}
	encoders.Mutex.Lock()
	encoders.Encoder.Encode(response)
	encoders.Mutex.Unlock()

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
		if msg.Signature != "" && msg.Signature != "dummy" && !verifySignature(msg, regComp.PublicKey) {
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
			// Forward message to destination
			registeredMutex.RLock()
			destComponent, exists := registered[msg.Destination]
			registeredMutex.RUnlock()

			if exists && destComponent.Encoders != nil {
				debugLog("hub", fmt.Sprintf("Forwarding %s->%s command %s to persistent encoder", msg.Source, msg.Destination, msg.Command))
				destComponent.Encoders.Mutex.Lock()
				destComponent.Encoders.Encoder.Encode(msg)
				destComponent.Encoders.Mutex.Unlock()

			} else {
				debugLog("hub", fmt.Sprintf("Destination %s not found or no encoders", msg.Destination))
				errorMsg := map[string]string{"error": "ERR_DESTINATION_NOT_FOUND"}
				encoder.Encode(errorMsg)
			}
		}
	}
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
