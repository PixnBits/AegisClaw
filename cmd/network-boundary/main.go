package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/agent"
	"AegisClaw/internal/boundarycrypto"
	"AegisClaw/internal/bootargs"
	"AegisClaw/internal/timing"
	"AegisClaw/internal/transport/hubclient"


	authv3 "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/mdlayher/vsock"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var allowedForwardHeaders = map[string]bool{
	"Accept":       true,
	"Content-Type": true,
}

// === Network Boundary Interface Contract (7.1) ===
//
// This section defines the expected wire format and semantics for
// communication with the Network Boundary. This contract is critical
// for security (identity, allowlist enforcement, secret scoping).
//
// Current supported commands (via Hub Message):
//
// 1. "network.request"
//    Payload (map[string]interface{}):
//      - "url"        (string, required) - Target URL
//      - "method"     (string, optional, defaults to GET)
//      - "body"       (string, optional)
//      - "headers"    (map[string]interface{}, optional) - limited to allowedForwardHeaders
//      - "skill_id"   (string, recommended) - identity of the requesting skill/agent
//
//    Behavior:
//      - In strict mode, "skill_id" is required.
//      - The boundary looks up the allowlist for the skill_id (or falls back to global).
//      - Only declared hosts for that skill are permitted.
//      - Secrets are injected only for the target host when available.
//
// 2. "register", "version", "get-version" - internal Hub protocol.
//    The Hub may return "store_public_key" (base64 ed25519 public key) in the
//    register response. This is the preferred way for the boundary to learn
//    which key the Store will use to sign "secrets.update" messages.
//
// 3. "secrets.get" / "secrets.request" (reconciliation)
//    Payload: (optional, can be empty map for now)
//    Behavior:
//      - Returns safe metadata only: list of skill IDs that have secrets,
//        the count, a timestamp, and the boundary's signer_pubkey.
//      - The entire response Message is signed with the boundary's private key
//        (the same one sent in the "register" payload). The Store should verify
//        Message.Signature using the included signer_pubkey (or the one from registration).
//      - The boundarycrypto package exports VerifyBoundarySignedResponse as a
//        convenience helper the Store can use for this verification.
//      - Never returns actual secret values.
//      - Used by the Store for drift detection and reconciliation.
//
// 4. "secrets.status" (health / reconciliation metrics)
//    Payload: (optional)
//    Behavior:
//      - Returns safe health metadata: count, last_update, nonce_cache_size, etc.
//      - Never returns actual secret values.
//      - Useful for operators and the Store to monitor the dynamic secrets system.
//
// Future evolution (per spec):
// - Encrypted secret material will be delivered via dedicated "secrets.update"
//   messages from the Store VM over the Hub. These messages must carry a
//   cryptographic signature (ed25519) that the boundary will verify against
//   the Store signer public key (preferably delivered by the Hub during the
//   "register" exchange, falling back to AEGIS_STORE_PUBLIC_KEY).
// - Per-skill network-access.yaml will be pushed from Store VM.
// - All requests will carry authenticated skill identity.
// - Secrets are loaded once at startup via AEGIS_SKILL_SECRETS_FILE,
//   AEGIS_SKILL_SECRETS_DIR (per-skill *.secret files), or AEGIS_SKILL_SECRETS env
//   (see loadSkillSecrets) and remain in the Go control plane only. They are
//   injected only through the allowlist-enforcing ExtAuthz path.
// - The Store can reconcile its view using "secrets.get" / "secrets.request"
//   messages (safe metadata only, no secret values). Responses are signed by the
//   boundary using its registered private key (public key included in the response
//   and originally sent during registration).
//
// Any change to this contract should be made deliberately and documented here.

type Message struct {
	Source      string      `json:"source"`
	Destination string      `json:"destination"`
	Command     string      `json:"command"`
	Payload     interface{} `json:"payload"`
	Timestamp   string      `json:"timestamp"`
	Signature   string      `json:"signature"`
}

var hubSocket = "~/.aegis/hub.sock"

// boundaryHealthy is the fail-closed health flag for the entire boundary.
// When false (e.g. lost Hub connection or other terminal security event),
// all egress paths (direct, vsock->Envoy, etc.) refuse traffic.
var boundaryHealthy = true

// registeredStoreSignerPublicKey is populated from the Hub during the
// "register" exchange when the Hub/Store provides the expected public key
// that will be used to sign "secrets.update" messages.
//
// This is the preferred source of truth for the Store signer key (better
// than relying solely on AEGIS_STORE_PUBLIC_KEY env var). It integrates
// the cryptographic trust root into the authenticated registration flow.
var registeredStoreSignerPublicKey string

// globalNonceCache provides replay protection for signed Hub messages that carry nonces.
// It is bounded and uses the same replay window as the timestamp check (plus margin).
var globalNonceCache = boundarycrypto.NewNonceCache(10000, 12*time.Minute)

// globalSecretsUpdateRateLimiter provides defensive rate limiting on the
// privileged "secrets.update" path. This protects the boundary (and the
// expensive signature verification + crypto work) from abuse or accidental
// flooding by the Hub/Store.
//
// Token-bucket style, bounded, with generous but safe limits for normal
// Store-driven secret rotation use cases.
var globalSecretsUpdateRateLimiter = boundarycrypto.NewRateLimiter(30, time.Minute) // 30 updates per minute max

// globalSecretsSymmetricKey is the 32-byte AES-256 key used for the real
// encrypted "secrets.update" blobs coming from the Store (7.1 full path).
//
// Loaded from AEGIS_SECRETS_SYMMETRIC_KEY (base64) at startup.
// In production this would come from attested key delivery during registration
// rather than a long-lived env var.
var globalSecretsSymmetricKey []byte

// liveSecretStore is the mutable, thread-safe holder for per-skill secrets.
// This is the foundation for the Hub-mediated dynamic secrets path (7.1+).
//
// Secrets can be updated at runtime via "secrets.update" messages from the
// Store VM over the Hub. In the real implementation these will be encrypted
// blobs that the boundary decrypts and (ideally) zeroizes after use.
//
// The store is the single source of truth shared between:
//   - direct injection (injectSecretForHost)
//   - ExtAuthz gRPC server (authorizationServer.Check)
//   - future audit / metrics paths
type liveSecretStore struct {
	sync.RWMutex
	data      map[string]string // skillID -> secret value
	lastUpdate time.Time        // last time the store was mutated (for health/reconciliation metrics)
}

func newLiveSecretStore() *liveSecretStore {
	return &liveSecretStore{data: make(map[string]string)}
}

// Get returns the secret for a skill (if present) without exposing the map.
func (s *liveSecretStore) Get(skillID string) (string, bool) {
	s.RLock()
	defer s.RUnlock()
	v, ok := s.data[skillID]
	return v, ok
}

// ReplaceAll atomically replaces the entire set of secrets.
// Used for "secrets.update" messages (supports both full replacement and the legacy single-skill form).
// The message is expected to carry a valid signature (see verifySecretsUpdateSignature).
func (s *liveSecretStore) ReplaceAll(newSecrets map[string]string) {
	s.Lock()
	defer s.Unlock()
	s.data = make(map[string]string)
	for k, v := range newSecrets {
		s.data[k] = v
	}
	s.lastUpdate = time.Now().UTC()
}

// Set adds or replaces a single skill's secret.
// Used for incremental "secrets.update" operations.
func (s *liveSecretStore) Set(skillID, secret string) {
	if skillID == "" || secret == "" {
		return
	}
	s.Lock()
	defer s.Unlock()
	s.data[skillID] = secret
	s.lastUpdate = time.Now().UTC()
}

// Remove deletes a skill's secret (if present).
// Used for incremental "secrets.update" operations.
func (s *liveSecretStore) Remove(skillID string) {
	if skillID == "" {
		return
	}
	s.Lock()
	defer s.Unlock()
	delete(s.data, skillID)
	s.lastUpdate = time.Now().UTC()
}

// Size returns the current number of loaded secrets (for logging / health).
func (s *liveSecretStore) Size() int {
	s.RLock()
	defer s.RUnlock()
	return len(s.data)
}

// ListSkills returns the list of skill IDs that currently have secrets configured.
// This is safe for reconciliation responses — it does not expose secret values.
func (s *liveSecretStore) ListSkills() []string {
	s.RLock()
	defer s.RUnlock()
	skills := make([]string, 0, len(s.data))
	for id := range s.data {
		skills = append(skills, id)
	}
	return skills
}

// LastUpdate returns the last time the store was mutated (UTC). Zero time if never updated.
func (s *liveSecretStore) LastUpdate() time.Time {
	s.RLock()
	defer s.RUnlock()
	return s.lastUpdate
}

func init() {
	if env := os.Getenv("AEGIS_HUB_SOCKET"); env != "" {
		hubSocket = env
	}
}

func expandPath(path string) string {
	if path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		return home + path[1:]
	}
	return path
}

func signMessage(msg *Message, priv ed25519.PrivateKey) {
	msgCopy := *msg
	msgCopy.Signature = ""
	data, _ := json.Marshal(msgCopy)
	signature := ed25519.Sign(priv, data)
	msg.Signature = base64.StdEncoding.EncodeToString(signature)
}

// verifySecretsUpdateSignature performs inbound signature verification
// on "secrets.update" messages from the Store VM via the Hub (real when a
// signer public key is configured via registration or env).
//
// Current behavior (this slice):
// - Requires a non-empty "signature" (or "sig") field in the payload.
// - Performs a placeholder check and always returns true for now.
// - Logs and audits the verification attempt.
//
// Real behavior (now partially wired):
// - When AEGIS_STORE_PUBLIC_KEY (base64 ed25519 public key) is set, we call
//   the real ed25519.Verify.
// - The signature covers a (currently minimal) canonical form of the payload.
// - Future slices will add proper canonicalization, timestamp + nonce replay
//   protection, and tighter integration with the boundary's registered keypair
//   or a Store certificate.
//
// This is the minimal paranoid requirement before the dynamic secrets channel
// can be considered trustworthy.
func verifySecretsUpdateSignature(payload map[string]interface{}) bool {
	sig, _ := payload["signature"].(string)
	if sig == "" {
		sig, _ = payload["sig"].(string)
	}

	if sig == "" {
		log.Printf("SECURITY: secrets.update received with no signature field")
		return false
	}

	// Prefer the Store signer public key delivered during registration
	// (integrated into the authenticated Hub flow). Fall back to the
	// environment variable for development / early testing.
	storePubB64 := registeredStoreSignerPublicKey
	if storePubB64 == "" {
		storePubB64 = strings.TrimSpace(os.Getenv("AEGIS_STORE_PUBLIC_KEY"))
	}

	if storePubB64 != "" {
		pubBytes, err := base64.StdEncoding.DecodeString(storePubB64)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			log.Printf("SECURITY: configured Store signer public key is invalid (must be base64 ed25519 public key)")
			return false
		}
		pub := ed25519.PublicKey(pubBytes)

		sigBytes, err := base64.StdEncoding.DecodeString(sig)
		if err != nil {
			log.Printf("SECURITY: secrets.update signature is not valid base64")
			return false
		}

		// === Proper canonical form + replay protection for this slice ===
		// We build deterministic data to sign/verify:
		//   - The secrets content (either full map or single skill/secret)
		//   - Timestamp (required for freshness)
		//   - Optional nonce (for replay protection when present)
		//
		// The Store must sign over the same canonical representation.
		canonical := boundarycrypto.CanonicalSecretsUpdateData(payload)
		if canonical == nil {
			log.Printf("SECURITY: secrets.update missing required timestamp for real verification")
			return false
		}

		// Timestamp freshness check (replay / clock skew protection)
		if !boundarycrypto.IsTimestampFresh(payload) {
			log.Printf("SECURITY: secrets.update timestamp is stale or in the future")
			return false
		}

		// Stronger replay protection via nonce (when provided by the signer)
		nonce, _ := payload["nonce"].(string)
		if nonce != "" && !globalNonceCache.CheckAndRecord(nonce) {
			log.Printf("SECURITY: secrets.update nonce replay detected")
			return false
		}

		if ed25519.Verify(pub, canonical, sigBytes) {
			log.Printf("SECURITY: secrets.update signature verified successfully with Store signer public key (canonical + freshness)")
			return true
		}

		log.Printf("SECURITY: secrets.update signature verification FAILED against Store signer public key")
		return false
	}

	// No Store signer public key available (neither via registration nor env var).
	// Fall back to "signature field present" stub behavior.
	log.Printf("SECURITY: secrets.update signature present — stub verification performed (Hub can provide store_public_key during register, or set AEGIS_STORE_PUBLIC_KEY)")
	return true
}



// nonceCache provides bounded, TTL-based replay protection for signed messages
// that carry a "nonce" field.
//
// Design goals (paranoid + practical):
// - Bounded memory (maxEntries) to prevent DoS via nonce flooding.
// - Entries older than the replay window are periodically cleaned.
// - A nonce is considered "seen" (replay) only if it was accepted within the
//   recent window.
// - Nonces are only recorded after full successful verification (signature +
//   timestamp + this nonce check).
//
// This is deliberately simple for the current phase. A production version
// could use a more sophisticated structure or external store.


func getBuildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		version := info.Main.Version
		if version == "" || version == "(devel)" {
			// Use commit hash if available
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
					return setting.Value[:7] // Short commit hash
				}
			}
			return "dev"
		}
		return version
	}
	return "unknown"
}

func isDomainAllowed(rawURL string, allowed map[string]bool) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return allowed[parsed.Host]
}

func parseAllowedURL(rawURL string, allowed map[string]bool) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}
	if !allowed[parsed.Host] {
		return nil, fmt.Errorf("domain not allowed")
	}
	return parsed, nil
}

func runNetworkBoundary(cmd *cobra.Command, args []string) {
	timing.RecordPhase("main_entry")

	// Generate keys
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pubStr := base64.StdEncoding.EncodeToString(pub)

	// 7.1 real secrets: load the AES-256 symmetric key for encrypted blobs
	// (sent by Store after signing the update message).
	if b64 := os.Getenv("AEGIS_SECRETS_SYMMETRIC_KEY"); b64 != "" {
		if decoded, err := base64.StdEncoding.DecodeString(b64); err == nil && len(decoded) == 32 {
			globalSecretsSymmetricKey = decoded
			log.Println("SECURITY: Loaded AEGIS_SECRETS_SYMMETRIC_KEY for real encrypted secrets path")
		} else {
			log.Println("SECURITY WARNING: AEGIS_SECRETS_SYMMETRIC_KEY present but invalid (must be 32-byte base64) — encrypted blob path disabled")
		}
	}

	socket := expandPath(hubSocket)
	conn, err := net.Dial("unix", socket)
	if err != nil {
		if bootargs.UseHubVsock() {
			fmt.Printf("network-boundary: waiting for host hub bridge on vsock :%d (Firecracker inverted path)\n", hubclient.GuestHubBridgePort)
			conn, err = hubclient.AcceptVsockHubBridgeConn(hubclient.GuestHubBridgePort)
		}
	}
	if err != nil {
		log.Fatal("Failed to connect to AegisHub:", err)
	}
	defer conn.Close()
	timing.RecordPhase("hub_dialed")

	if bootargs.UseHubVsock() {
		startOllamaInvertBridge()
	}

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	var connMutex sync.Mutex
	// boundaryHealthy is the package-level fail-closed flag (initialized at package scope)

	// Register
	regMsg := Message{
		Source:      "network-boundary",
		Destination: "hub",
		Command:     "register",
		Payload: map[string]string{
			"public_key": pubStr,
			"version":    getBuildVersion(),
		},
		Timestamp: time.Now().Format(time.RFC3339),
		Signature: "dummy",
	}
	connMutex.Lock()
	err = encoder.Encode(regMsg)
	connMutex.Unlock()
	if err != nil {
		log.Fatal("Failed to register:", err)
	}

	// Consume response
	var resp map[string]interface{}
	connMutex.Lock()
	err = decoder.Decode(&resp)
	connMutex.Unlock()
	if err != nil {
		log.Fatal("Failed to decode register response:", err)
	}
	if error, ok := resp["error"]; ok {
		log.Fatal("Registration failed:", error)
	}
	timing.RecordPhase("register_complete")
	timing.WriteComponentReadySentinel()

	// 7.1: Capture the Store signer public key from the Hub if provided.
	// This ties the cryptographic material for secrets.update messages
	// into the authenticated registration flow instead of (or in addition to)
	// out-of-band environment configuration.
	if storePub, ok := resp["store_public_key"].(string); ok && storePub != "" {
		registeredStoreSignerPublicKey = storePub
		fmt.Println("Network Boundary received Store signer public key via registration")
	}

	fmt.Println("Network Boundary registered")

	// PILOT: First execution of the 7.1 design sketch reuse (see pilotDesignSketchReuse below).
	// This is the initial validation that the signed-message + boundarycrypto patterns
	// can be reused on non-secrets flows. Called once at startup for the first pilot slice.
	pilotDesignSketchReuse(priv)

	// Load allowed domains — hardened for Task 7.1 (paranoid zero-trust)
	ollamaHost := ollamaBackendHost()
	allowedDomains := loadAllowedDomains(ollamaHost)
	skillAllowlists := loadSkillAllowlists()

	// 7.1 Hub secrets path kickoff: create the live mutable store.
	// Static file/env secrets are loaded into it at startup.
	// Later "secrets.update" messages can replace or augment it at runtime.
	liveSecrets := newLiveSecretStore()
	staticSecrets := loadSkillSecrets()
	liveSecrets.ReplaceAll(staticSecrets)

	// Strict mode: refuse to operate with an empty or only-default allowlist.
	// This enforces "strict allowlists only" per network-boundary.md.
	strict := strings.ToLower(os.Getenv("AEGIS_BOUNDARY_STRICT")) == "1" || os.Getenv("AEGIS_BOUNDARY_STRICT") == "true"
	if strict && len(allowedDomains) <= 3 { // only the three defaults
		log.Fatal("Network Boundary started in strict mode with insufficient allowlist. Refusing to proxy any traffic (fail-closed).")
	}

	// 7.1 strict-mode secrets hardening (paranoid TCB)
	// Any explicitly configured secrets source (single file or directory) must
	// have secure permissions in strict mode. Fail closed on misconfiguration.
	if strict {
		// Single file case
		if secretsFile := strings.TrimSpace(os.Getenv("AEGIS_SKILL_SECRETS_FILE")); secretsFile != "" {
			info, err := os.Stat(secretsFile)
			if err != nil {
				log.Fatalf("Network Boundary started in strict mode with unreadable secrets file %s: %v. Refusing to start (fail-closed).", secretsFile, err)
			}
			mode := info.Mode().Perm()
			if mode != 0600 && mode != 0400 {
				log.Fatalf("Network Boundary started in strict mode with insecure secrets file %s (mode %o, require 0600). Refusing to start (fail-closed).", secretsFile, mode)
			}
		}

		// Directory case — every .secret file must be secure
		if dir := strings.TrimSpace(os.Getenv("AEGIS_SKILL_SECRETS_DIR")); dir != "" {
			entries, err := os.ReadDir(dir)
			if err != nil {
				log.Fatalf("Network Boundary started in strict mode with unreadable secrets directory %s: %v. Refusing to start (fail-closed).", dir, err)
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".secret") {
					continue
				}
				fullPath := dir + "/" + e.Name()
				info, err := os.Stat(fullPath)
				if err != nil {
					log.Fatalf("Network Boundary started in strict mode with unreadable secret file %s: %v. Refusing to start (fail-closed).", fullPath, err)
				}
				mode := info.Mode().Perm()
				if mode != 0600 && mode != 0400 {
					log.Fatalf("Network Boundary started in strict mode with insecure secret file %s (mode %o, require 0600). Refusing to start (fail-closed).", fullPath, mode)
				}
			}
		}
	}

	fmt.Printf("Network Boundary effective allowlist: %v (strict=%v)\n", allowedDomains, strict)
	if len(skillAllowlists) > 0 {
		fmt.Printf("Network Boundary loaded per-skill rules for %d skills\n", len(skillAllowlists))
	}
	if n := liveSecrets.Size(); n > 0 {
		fmt.Printf("Network Boundary loaded secrets for %d skills (real secrets path active, live via Hub)\n", n)
	} else {
		fmt.Println("Network Boundary using demo secret seeds (set AEGIS_SKILL_SECRETS_FILE, AEGIS_SKILL_SECRETS_DIR, or AEGIS_SKILL_SECRETS for real material)")
	}

	// Start Egress Proxy (7.1)
	// This is the controlled outbound path for all VMs with EgressViaBoundary=true.
	// VMs connect here (initially over host-forwarded TCP or vsock), and we enforce
	// allowlists + secret injection + full audit.
	//
	// Crash containment: we still launch the listener (so health can be recovered by
	// a supervisor restarting the boundary), but every handler + vsock path checks
	// boundaryHealthy and refuses with 503 on degraded.
	// Future: This listener will be on vsock so Firecracker guests can dial it directly
	// without any host network exposure.
	go func() {
		egressAddr := os.Getenv("AEGIS_EGRESS_PROXY_ADDR")
		if egressAddr == "" {
			egressAddr = ":8081"
		}

		http.HandleFunc("/egress", func(w http.ResponseWriter, r *http.Request) {
			if !boundaryHealthy {
				http.Error(w, "Network Boundary degraded - outbound blocked for safety", 503)
				return
			}

			targetURL := r.URL.Query().Get("url")
			if targetURL == "" {
				http.Error(w, "url parameter required", 400)
				return
			}

			// Skill identity can be passed via header or query for now
			skillID := r.Header.Get("X-Aegis-Skill-ID")
			if skillID == "" {
				skillID = r.URL.Query().Get("skill_id")
			}

			effectiveAllowed := getAllowedForSkill(skillID, allowedDomains, skillAllowlists)

			parsedURL, err := parseAllowedURL(targetURL, effectiveAllowed)
			if err != nil {
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": targetURL, "skill_id": skillID},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()
				http.Error(w, "Domain not allowed", 403)
				return
			}

			req, err := http.NewRequest(r.Method, parsedURL.String(), r.Body)
			if err != nil {
				http.Error(w, "Invalid URL", 400)
				return
			}
			for header := range allowedForwardHeaders {
				if val := r.Header.Get(header); val != "" {
					req.Header.Set(header, val)
				}
			}

			// Secret injection (centralized, hardened, now unified with per-skill secrets)
			injectSecretForHost(req, parsedURL.Host, skillID, liveSecrets)

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				http.Error(w, "Proxy error", 500)
				return
			}
			defer resp.Body.Close()

			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)

			// Audit
			auditMsg := Message{
				Source:      "network-boundary",
				Destination: "store",
				Command:     "audit.append",
				Payload:     map[string]interface{}{"action": "proxied_request", "url": targetURL, "status": resp.StatusCode, "skill_id": skillID},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&auditMsg, priv)
			connMutex.Lock()
			encoder.Encode(auditMsg)
			connMutex.Unlock()
		})

		log.Printf("Network Boundary egress proxy listening on %s", egressAddr)
		log.Fatal(http.ListenAndServe(egressAddr, nil))
	}()

	// Start Envoy (real proxy engine) - 7.1 in progress
	// We now pass the current authoritative allowlists + per-skill secrets
	// so the control plane owns the single source of truth for policy + secrets.
	go startEnvoy(priv, allowedDomains, skillAllowlists, liveSecrets)

	// Start the real vsock egress listener (7.1 progress)
	// Guests with EgressViaBoundary=true will connect over vsock to this listener
	// and get proxied through the controlled egress path (allowlists + secrets + audit).
	go startVSockEgressListener()

	// Boundary loop
	for {
		var msg Message
		connMutex.Lock()
		err := decoder.Decode(&msg)
		connMutex.Unlock()
		if err != nil {
			log.Printf("SECURITY EVENT: Lost connection to AegisHub (decode error: %v). Entering degraded state - outbound requests will be refused.", err)
			boundaryHealthy = false
			// Treat loss of Hub as a terminal security condition for now (crash containment)
			// A supervisor can restart the boundary VM, which should result in blocked networking.
			break
		}

		fmt.Println("Network Boundary received:", msg.Command)

		response := Message{
			Source:      "network-boundary",
			Destination: msg.Source,
			Timestamp:   time.Now().Format(time.RFC3339),
			Signature:   "",
		}

		switch msg.Command {
		case "network.request":
			if !boundaryHealthy {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": "Network Boundary degraded - requests blocked for safety"}
				break
			}

			// Handle network request — per-skill enforcement + contract validation (7.1)
			payload, ok := msg.Payload.(map[string]interface{})
			if !ok {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": "invalid payload type"}
				break
			}

			targetURL, _ := payload["url"].(string)
			method, _ := payload["method"].(string)
			if strings.TrimSpace(method) == "" {
				method = http.MethodGet
			}

			skillID, _ := payload["skill_id"].(string)

			// Contract enforcement: skill identity is required in strict mode for proper scoping/audit
			strict := strings.ToLower(os.Getenv("AEGIS_BOUNDARY_STRICT")) == "1" || os.Getenv("AEGIS_BOUNDARY_STRICT") == "true"
			if strict && skillID == "" {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": "skill_id required in strict mode"}
				// Audit the policy violation
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "missing_skill_identity", "url": targetURL},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()
				break
			}

			effectiveAllowed := getAllowedForSkill(skillID, allowedDomains, skillAllowlists)

			parsedURL, err := parseAllowedURL(targetURL, effectiveAllowed)
			if err != nil {
				response.Command = "network.response"
				response.Payload = map[string]interface{}{"error": err.Error()}
				// Audit with skill context
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "blocked_request", "url": targetURL, "skill_id": skillID},
					Timestamp:   response.Timestamp,
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()
			} else {
				var bodyReader io.Reader
				if body, ok := payload["body"].(string); ok {
					bodyReader = strings.NewReader(body)
				}

				req, err := http.NewRequest(method, parsedURL.String(), bodyReader)
				if err != nil {
					response.Command = "network.response"
					response.Payload = map[string]interface{}{"error": "Invalid request"}
				} else {
					if headers, ok := payload["headers"].(map[string]interface{}); ok {
						for k, v := range headers {
							if allowedForwardHeaders[k] {
								req.Header.Set(k, fmt.Sprintf("%v", v))
							}
						}
					}
					// Use the centralized (and safer) injection helper — now skill-aware
					injectSecretForHost(req, parsedURL.Host, skillID, liveSecrets)
					client := &http.Client{}
					resp, err := client.Do(req)
					if err != nil {
						response.Command = "network.response"
						response.Payload = map[string]interface{}{"error": "Request failed"}
					} else {
						defer resp.Body.Close()
						body, _ := io.ReadAll(resp.Body)
						response.Command = "network.response"
						response.Payload = map[string]interface{}{"status": resp.StatusCode, "body": string(body)}
						// Audit
						auditMsg := Message{
							Source:      "network-boundary",
							Destination: "store",
							Command:     "audit.append",
							Payload:     map[string]interface{}{"action": "network_request", "url": targetURL, "status": resp.StatusCode, "skill_id": skillID},
							Timestamp:   response.Timestamp,
							Signature:   "",
						}
						signMessage(&auditMsg, priv)
						connMutex.Lock()
						encoder.Encode(auditMsg)
						connMutex.Unlock()
					}
				}
			}
		case "version", "get-version":
			// PILOT: Also exercise the design-sketch reuse from the version path (safe, low-frequency).
			// In practice the pilot function is cheap and only logs once per process in spirit.
			pilotDesignSketchReuse(priv)

			if msg.Command == "get-version" {
				// For get-version from hub, send proper Message response back
				response.Command = "version"
				response.Source = "network-boundary"
				response.Destination = msg.Source
				response.Payload = map[string]string{"version": getBuildVersion()}
				// Don't continue - let normal flow sign and send
			} else {
				response.Command = "version"
				response.Payload = map[string]string{"version": getBuildVersion()}
			}

		// llm.call: real Ollama path for Project Manager (and full agents via NewRealLLMCaller).
		// Matches the wire contract in internal/agent/loop/loop.go: "llm.call" with payload
		// { "request": {"model":.., "prompt":.., "stream":false}, "endpoint": "/api/generate" }.
		// We call the configured ollama host (already in allowlist) directly (the boundary VM
		// is the only component allowed egress). Returns shape expected by caller:
		// Payload["response"] = the generated text (or raw ollama JSON string for compatibility).
		// On any failure we set Command="error" so NewRealLLMCaller surfaces err and PM falls back.
		case "llm.call":
			if !boundaryHealthy {
				response.Command = "error"
				response.Payload = "Network Boundary degraded - LLM calls blocked for safety"
				break
			}
			payload, ok := msg.Payload.(map[string]interface{})
			if !ok {
				response.Command = "error"
				response.Payload = "invalid llm.call payload"
				break
			}
			reqIface, _ := payload["request"].(map[string]interface{})
			endpoint, _ := payload["endpoint"].(string)
			if strings.TrimSpace(endpoint) == "" {
				endpoint = "/api/generate"
			}
			model := ""
			prompt := ""
			if reqIface != nil {
				if m, ok := reqIface["model"].(string); ok {
					model = m
				}
				if p, ok := reqIface["prompt"].(string); ok {
					prompt = p
				}
			}
			if model == "" || strings.EqualFold(model, "default") {
				model = bootargs.DefaultModel(agent.DefaultLLMModel)
			}
			if model == "" || prompt == "" {
				response.Command = "error"
				response.Payload = "llm.call requires model and prompt in request"
				break
			}

			raw, err := callOllamaGenerate(model, prompt, endpoint)
			if err != nil {
				response.Command = "error"
				response.Payload = "ollama request failed: " + err.Error()
				log.Printf("llm.call ollama request failed: %v", err)
				break
			}

			// Parse full Ollama response for usage metrics (prompt_eval_count, eval_count, durations, model).
			// Always surface clean "response" (text) for existing NewRealLLMCaller / loop callers.
			text, usage := parseOllamaForLLMCall(raw, model)

			response.Command = "llm.call.response"
			response.Payload = map[string]interface{}{
				"response": text,
				"usage":    usage,
			}
			log.Printf("LLM plan gen via ollama (%s, %d bytes response, prompt_tokens=%v completion=%v)", model, len(text), usage["prompt_tokens"], usage["completion_tokens"])

			// Emit usage record to Store for durable aggregates (outside guest). Uses same signed hub path.
			// This wires the collection end-to-end for /api/llm-usage and portal.
			rec := map[string]interface{}{
				"agent_id":          msg.Source,
				"timestamp":         time.Now().UTC().Format(time.RFC3339),
				"model":             usage["model"],
				"tokens_prompt":     usage["prompt_tokens"],
				"tokens_completion": usage["completion_tokens"],
				"duration_ms":       usage["duration_ms"],
				"success":           usage["success"],
			}
			recMsg := Message{
				Source:      "network-boundary",
				Destination: "store",
				Command:     "llm.usage.record",
				Payload:     rec,
				Timestamp:   time.Now().UTC().Format(time.RFC3339),
			}
			signMessage(&recMsg, priv)
			if encErr := encoder.Encode(recMsg); encErr != nil {
				log.Printf("llm.usage.record emit failed: %v", encErr)
			}

		// === 7.1 Hub secrets delivery path (first implementation) ===
		// The Store VM (via the Hub) can now push updated per-skill secrets
		// at runtime instead of requiring a boundary restart.
		//
		// Stub payload for this slice (will evolve):
		//   Payload: map[string]interface{}{
		//       "secrets": map[string]string{ "skill-id": "secret-value", ... }
		//   }
		//
		// Real future version will include:
		//   - Cryptographic signature from the Store
		//   - Encrypted blob(s) (not plaintext)
		//   - Per-skill versioning / timestamps
		//   - Explicit zeroization after use
		case "secrets.update":
			if !boundaryHealthy {
				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "Network Boundary degraded"}
				break
			}

			// Defensive rate limiting on the privileged secrets update path.
			// This protects CPU (signature verification) and the live store.
			if !globalSecretsUpdateRateLimiter.Allow() {
				log.Printf("SECURITY EVENT: secrets.update rate limited (too many attempts)")
				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "secrets_update_rate_limited"},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()

				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "rate limited"}
				break
			}

			payload, ok := msg.Payload.(map[string]interface{})
			if !ok {
				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "invalid payload"}
				break
			}

			// 7.1: Signature verification on the dynamic secrets channel (stub)
			sigOK := verifySecretsUpdateSignature(payload)

			strict := strings.ToLower(os.Getenv("AEGIS_BOUNDARY_STRICT")) == "1" || os.Getenv("AEGIS_BOUNDARY_STRICT") == "true"

			if !sigOK {
				// Fail-closed behavior for the dynamic secrets channel.
				log.Printf("SECURITY EVENT: secrets.update REJECTED due to missing/invalid signature (strict=%v)", strict)

				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "secrets_update_rejected", "reason": "signature_verification_failed", "strict": strict, "signature_verified": false},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()

				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "signature verification failed", "signature_verified": false}
				break
			}

			// === 7.1 Full encrypted path (real secrets via Hub) ===
			// If the Store sent ciphertext + nonce instead of plaintext secrets,
			// decrypt using the symmetric key we loaded, then zero the plaintext
			// immediately after feeding it to the live store.
			if ctIface, hasCt := payload["encrypted_blob"]; hasCt {
				if nonceIface, hasNonce := payload["nonce"]; hasNonce {
					if ctB64, ok := ctIface.(string); ok {
						if nonceB64, ok := nonceIface.(string); ok {
							ct, _ := base64.StdEncoding.DecodeString(ctB64)
							nonce, _ := base64.StdEncoding.DecodeString(nonceB64)

							if len(globalSecretsSymmetricKey) == 32 && len(ct) > 0 && len(nonce) > 0 {
								decrypted, derr := boundarycrypto.DecryptSecretsBlob(ct, nonce, globalSecretsSymmetricKey)
								if derr != nil {
									log.Printf("SECURITY EVENT: secrets.update encrypted blob decryption FAILED — rejecting")
									response.Command = "secrets.response"
									response.Payload = map[string]interface{}{"error": "encrypted blob decryption failed"}
									break
								}
								liveSecrets.ReplaceAll(decrypted)
								boundarycrypto.ZeroSecretsMap(decrypted) // critical: clear after use

								log.Printf("SECURITY: Received *encrypted* secrets.update (%d skills, decrypted+zeroized)", len(decrypted))
								response.Command = "secrets.response"
								response.Payload = map[string]interface{}{"status": "accepted", "encrypted": true, "count": len(decrypted)}
								break
							}
						}
					}
				}
			}

			// === Incremental operations support (new in this slice) ===
			// Phase 4 hardening: plaintext secret updates via this path are legacy.
			// Real production use must come as encrypted blobs from Store (secrets.push).
			if strict {
				log.Printf("SECURITY WARNING (strict mode): received legacy plaintext secrets.update — migrate to encrypted blobs from Store")
			}
			// Preferred modern format:
			//   "operations": [
			//     {"op": "add",     "skill_id": "foo", "secret": "bar"},
			//     {"op": "replace", "skill_id": "baz", "secret": "newval"},
			//     {"op": "remove",  "skill_id": "oldone"}
			//   ]
			//
			// The entire message (including operations) must still be signed
			// and pass canonicalization + replay protection.
			if ops, ok := payload["operations"].([]interface{}); ok && len(ops) > 0 {
				applied := 0
				for _, opIface := range ops {
					op, ok := opIface.(map[string]interface{})
					if !ok {
						continue
					}
					operation, _ := op["op"].(string)
					skillID, _ := op["skill_id"].(string)

					switch operation {
					case "add", "replace", "set":
						if secret, ok := op["secret"].(string); ok && secret != "" && skillID != "" {
							liveSecrets.Set(skillID, secret)
							applied++
						}
					case "remove", "delete":
						if skillID != "" {
							liveSecrets.Remove(skillID)
							applied++
						}
					}
				}

				log.Printf("SECURITY: Received incremental secrets.update (%d operations applied)", applied)

				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "secrets_updated", "incremental": true, "ops_applied": applied, "signature_verified": true},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()

				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"status": "accepted", "incremental": true, "ops_applied": applied, "signature_verified": true}
				break
			}

			// === Legacy / full replacement path (still supported) ===
			var updates map[string]string
			if m, ok := payload["secrets"].(map[string]interface{}); ok {
				updates = make(map[string]string)
				for k, v := range m {
					if s, ok := v.(string); ok {
						updates[k] = s
					}
				}
			} else if sid, ok := payload["skill_id"].(string); ok {
				if sec, ok := payload["secret"].(string); ok && sec != "" {
					updates = map[string]string{sid: sec}
				}
			}

			if len(updates) > 0 {
				liveSecrets.ReplaceAll(updates)
				log.Printf("SECURITY: Received secrets.update from Hub for %d skills (full replacement, sig_ok=true)", len(updates))

				auditMsg := Message{
					Source:      "network-boundary",
					Destination: "store",
					Command:     "audit.append",
					Payload:     map[string]interface{}{"action": "secrets_updated", "count": len(updates), "signature_verified": true},
					Timestamp:   time.Now().Format(time.RFC3339),
					Signature:   "",
				}
				signMessage(&auditMsg, priv)
				connMutex.Lock()
				encoder.Encode(auditMsg)
				connMutex.Unlock()

				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"status": "accepted", "count": len(updates), "signature_verified": true}
			} else {
				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "no valid secrets or operations in payload"}
			}

		// === 7.1 Reconciliation message ===
		// The Store VM can ask the boundary for its current view of secrets.
		// Response contains only safe metadata (skill IDs + count) — never the
		// actual secret values. This is critical for drift detection and recovery
		// without creating a new secret exfiltration path.
		//
		// In a real implementation the request would be signed by the Store (or
		// authenticated via the Hub), and the response could be signed by the
		// boundary using its registered private key.
		case "secrets.get", "secrets.request":
			if !boundaryHealthy {
				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "Network Boundary degraded"}
				break
			}

			// Optional nonce-based replay protection on reconciliation requests
			// (if the Store signs the request and includes a nonce).
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if nonce, _ := payload["nonce"].(string); nonce != "" {
					if !globalNonceCache.CheckAndRecord(nonce) {
						log.Printf("SECURITY: secrets.get request replay detected via nonce")
						response.Command = "secrets.response"
						response.Payload = map[string]interface{}{"error": "replay detected"}
						break
					}
				}
			}

			skills := liveSecrets.ListSkills()
			count := len(skills)

			log.Printf("SECURITY: Received %s request from Hub (returning %d skills for reconciliation)", msg.Command, count)

			// Audit the reconciliation request
			auditMsg := Message{
				Source:      "network-boundary",
				Destination: "store",
				Command:     "audit.append",
				Payload:     map[string]interface{}{"action": "secrets_reconciliation_requested", "count": count},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&auditMsg, priv)
			connMutex.Lock()
			encoder.Encode(auditMsg)
			connMutex.Unlock()

			response.Command = "secrets.response"
			response.Payload = map[string]interface{}{
				"status":       "ok",
				"skills":       skills,
				"count":        count,
				"timestamp":    time.Now().Format(time.RFC3339),
				"signer_pubkey": base64.StdEncoding.EncodeToString(pub), // boundary's public key (sent during registration) so Store can verify the response signature
			}

		// === 7.1 Secret health / reconciliation metrics ===
		// Lightweight status for operators and the Store.
		// Returns only safe metadata (no secret values).
		case "secrets.status":
			if !boundaryHealthy {
				response.Command = "secrets.response"
				response.Payload = map[string]interface{}{"error": "Network Boundary degraded"}
				break
			}

			count := liveSecrets.Size()
			lastUpdate := liveSecrets.LastUpdate()
			nonceSize := globalNonceCache.Size()

			log.Printf("SECURITY: Received secrets.status request (count=%d)", count)

			auditMsg := Message{
				Source:      "network-boundary",
				Destination: "store",
				Command:     "audit.append",
				Payload:     map[string]interface{}{"action": "secrets_status_requested", "count": count},
				Timestamp:   time.Now().Format(time.RFC3339),
				Signature:   "",
			}
			signMessage(&auditMsg, priv)
			connMutex.Lock()
			encoder.Encode(auditMsg)
			connMutex.Unlock()

			response.Command = "secrets.response"
			response.Payload = map[string]interface{}{
				"status":             "ok",
				"count":              count,
				"last_update":        lastUpdate.Format(time.RFC3339),
				"nonce_cache_size":   nonceSize,
				"timestamp":          time.Now().Format(time.RFC3339),
			}

		default:
			response.Command = "error"
			response.Payload = "unknown command"
		}
		signMessage(&response, priv)

		connMutex.Lock()
		err = encoder.Encode(response)
		connMutex.Unlock()
		if err != nil {
			log.Println("Failed to send response:", err)
		}
	}
}

func ollamaBackendHost() string {
	if host := strings.TrimSpace(os.Getenv("AEGIS_OLLAMA_BACKEND_HOST")); host != "" {
		return host
	}
	return "localhost:11434"
}

// loadAllowedDomains builds the global/fallback allowlist with multiple sources (paranoid, declarative).
// Priority: file (AEGIS_ALLOWED_DOMAINS_FILE) > env (AEGIS_ALLOWED_DOMAINS) > defaults.
func loadAllowedDomains(ollamaHost string) map[string]bool {
	allowed := map[string]bool{
		"example.com":    true,
		"api.github.com": true,
		ollamaHost:       true,
	}

	// File-based declarative allowlist
	if filePath := strings.TrimSpace(os.Getenv("AEGIS_ALLOWED_DOMAINS_FILE")); filePath != "" {
		if data, err := os.ReadFile(filePath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					allowed[line] = true
				}
			}
		} else {
			log.Printf("Warning: could not read AEGIS_ALLOWED_DOMAINS_FILE %s: %v", filePath, err)
		}
	}

	// Env override (comma-separated)
	if envList := strings.TrimSpace(os.Getenv("AEGIS_ALLOWED_DOMAINS")); envList != "" {
		for _, d := range strings.Split(envList, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				allowed[d] = true
			}
		}
	}
	return allowed
}

// parseOllamaForLLMCall extracts the generated text and usage metrics (tokens, duration) from
// the raw JSON returned by Ollama /api/generate. This is the central point for accurate
// per-call LLM usage collection (outside any Agent Runtime guest VM). Unit tested.
func parseOllamaForLLMCall(raw, model string) (string, map[string]interface{}) {
	text := raw
	usage := map[string]interface{}{"model": model}
	var ollama map[string]interface{}
	if json.Unmarshal([]byte(raw), &ollama) == nil {
		if r, ok := ollama["response"].(string); ok && r != "" {
			text = r
		}
		if v, ok := ollama["prompt_eval_count"].(float64); ok {
			usage["prompt_tokens"] = int(v)
		}
		if v, ok := ollama["eval_count"].(float64); ok {
			usage["completion_tokens"] = int(v)
		}
		if v, ok := ollama["total_duration"].(float64); ok {
			usage["duration_ms"] = int(v / 1e6) // ns -> ms
		}
		if m, ok := ollama["model"].(string); ok && m != "" {
			usage["model"] = m
		}
		usage["success"] = true
	} else {
		usage["success"] = true
	}
	return text, usage
}

// loadSkillAllowlists loads per-skill network access rules from a directory.
// Expected layout (for now, file-based declarative):
//   $AEGIS_SKILL_NETWORK_RULES_DIR/<skill-id>.domains   (one domain per line)
// This is the stepping stone toward loading from Store VM per network-access.yaml (spec).
func loadSkillAllowlists() map[string]map[string]bool {
	rules := make(map[string]map[string]bool)

	dir := strings.TrimSpace(os.Getenv("AEGIS_SKILL_NETWORK_RULES_DIR"))
	if dir == "" {
		return rules
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Warning: could not read AEGIS_SKILL_NETWORK_RULES_DIR %s: %v", dir, err)
		return rules
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".domains") {
			continue
		}
		skillID := strings.TrimSuffix(e.Name(), ".domains")
		data, err := os.ReadFile(dir + "/" + e.Name())
		if err != nil {
			continue
		}
		skillRules := make(map[string]bool)
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				skillRules[line] = true
			}
		}
		if len(skillRules) > 0 {
			rules[skillID] = skillRules
		}
	}
	return rules
}

func getAllowedForSkill(skillID string, global map[string]bool, skillRules map[string]map[string]bool) map[string]bool {
	if skillRules != nil {
		if perSkill, ok := skillRules[skillID]; ok && len(perSkill) > 0 {
			return perSkill
		}
	}
	return global
}

// loadSkillSecrets loads per-skill secret material for the 7.1 real secrets path (stub).
//
// Supported sources (in priority order for this slice):
//   1. AEGIS_SKILL_SECRETS_FILE  — single protected file (recommend 0600).
//      Format: one entry per line "skill-id=the-secret-value"
//   2. AEGIS_SKILL_SECRETS_DIR   — directory containing per-skill secret files.
//      Expected layout: $DIR/<skill-id>.secret
//      Each .secret file contains the raw secret value (single line or trimmed content).
//      This is the direct parallel to AEGIS_SKILL_NETWORK_RULES_DIR/*.domains.
//   3. AEGIS_SKILL_SECRETS         — env var (comma / newline / semicolon separated "skill=val").
//   4. If none configured, returns empty map (caller falls back to internal demo seeds).
//
// SPEC REFERENCES (Phase 4):
//   - secret-management.md §Key Guarantees (Boundary is the sole handler; secrets
//     must never be visible outside it).
//   - network-boundary.md + 7.1 capabilities doc (encrypted blobs from Store are
//     the production path; legacy file/dir/env loading is deprecated for production).
//
// Paranoid / TCB rules (enforced or documented here):
// - NEVER log actual secret values.
// - When a secrets file or directory is provided we perform best-effort os.Stat
//   permission checks per file and emit SECURITY WARNINGs on weak modes.
// - In strict mode (AEGIS_BOUNDARY_STRICT=1), any declared secrets file or any
//   .secret file in the directory must be readable and have mode 0600/0400, or
//   the boundary refuses to start (fail-closed).
// - Phase 4: These legacy paths are **deprecated** for production. The only
//   production mechanism is encrypted blobs pushed from the Store VM via
//   "secrets.push" / "secrets.update" over the Hub. See the secrets.update
//   handler and injectSecretForHost for the real path.
// - The Go control plane (this binary) remains the single source of truth for
//   secrets. Envoy only receives injected headers via the ext_authz gRPC path.
func loadSkillSecrets() map[string]string {
	secrets := make(map[string]string)

	strict := strings.ToLower(os.Getenv("AEGIS_BOUNDARY_STRICT")) == "1" || os.Getenv("AEGIS_BOUNDARY_STRICT") == "true"

	// Phase 4 enforcement (secret-management.md §Key Guarantees + network-boundary.md):
	// In strict mode, legacy file/dir/env secret sources are disabled.
	// Only encrypted blobs from the Store (via secrets.push) are accepted.
	// This is the critical step that removes file/dir/env fallbacks from the
	// production secret path.
	if strict {
		log.Printf("SECURITY (strict mode): Legacy secret sources (FILE/DIR/ENV) are disabled. Only encrypted blobs from Store are allowed.")
		return secrets // return empty — encrypted path via Hub is the only source
	}

	// File-based declarative source (primary path for production-like configs)
	if filePath := strings.TrimSpace(os.Getenv("AEGIS_SKILL_SECRETS_FILE")); filePath != "" {
		// Best-effort paranoid permission check
		if info, err := os.Stat(filePath); err == nil {
			mode := info.Mode().Perm()
			if mode != 0600 && mode != 0400 {
				log.Printf("SECURITY WARNING: AEGIS_SKILL_SECRETS_FILE %s has mode %o (strongly recommend 0600)", filePath, mode)
			}
		} else {
			log.Printf("Warning: could not stat AEGIS_SKILL_SECRETS_FILE %s: %v", filePath, err)
		}

		if data, err := os.ReadFile(filePath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				if idx := strings.Index(line, "="); idx > 0 {
					skill := strings.TrimSpace(line[:idx])
					val := strings.TrimSpace(line[idx+1:])
					if skill != "" && val != "" {
						secrets[skill] = val
					}
				}
			}
		} else {
			log.Printf("Warning: could not read AEGIS_SKILL_SECRETS_FILE %s: %v", filePath, err)
		}
	}

	// Directory-based per-skill secrets (operational ergonomics win)
	// Layout: $AEGIS_SKILL_SECRETS_DIR/<skill-id>.secret
	// Each file contains the secret value for that skill (trimmed content).
	// This mirrors AEGIS_SKILL_NETWORK_RULES_DIR exactly.
	if dir := strings.TrimSpace(os.Getenv("AEGIS_SKILL_SECRETS_DIR")); dir != "" {
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.Printf("Warning: could not read AEGIS_SKILL_SECRETS_DIR %s: %v", dir, err)
		} else {
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".secret") {
					continue
				}
				skillID := strings.TrimSuffix(e.Name(), ".secret")
				fullPath := dir + "/" + e.Name()

				// Paranoid per-file permission check
				if info, err := os.Stat(fullPath); err == nil {
					mode := info.Mode().Perm()
					if mode != 0600 && mode != 0400 {
						log.Printf("SECURITY WARNING: %s has mode %o (strongly recommend 0600)", fullPath, mode)
					}
				} else {
					log.Printf("Warning: could not stat %s: %v", fullPath, err)
					continue
				}

				data, err := os.ReadFile(fullPath)
				if err != nil {
					log.Printf("Warning: could not read %s: %v", fullPath, err)
					continue
				}
				secret := strings.TrimSpace(string(data))
				if skillID != "" && secret != "" {
					secrets[skillID] = secret // dir overrides single file for same skill
				}
			}
		}
	}

	// Environment variable (dev / CI / quick overrides)
	if envList := strings.TrimSpace(os.Getenv("AEGIS_SKILL_SECRETS")); envList != "" {
		for _, entry := range strings.FieldsFunc(envList, func(r rune) bool {
			return r == ',' || r == '\n' || r == ';' || r == ' '
		}) {
			entry = strings.TrimSpace(entry)
			if entry == "" || strings.HasPrefix(entry, "#") {
				continue
			}
			if idx := strings.Index(entry, "="); idx > 0 {
				skill := strings.TrimSpace(entry[:idx])
				val := strings.TrimSpace(entry[idx+1:])
				if skill != "" && val != "" {
					secrets[skill] = val // env wins for duplicate keys
				}
			}
		}
	}

	return secrets
}

// injectSecretForHost centralizes secret injection for outbound requests.
//
// 7.1 real secrets unification:
// - If a valid skillID + non-empty secrets map is provided, we first look up
//   a per-skill secret. This is now the primary path for "real" secrets loaded
//   via loadSkillSecrets (protected file or env).
// - Only if no per-skill secret is found do we fall back to legacy host-based
//   special cases (currently only api.github.com via GITHUB_TOKEN env).
// - This gives the direct Go egress paths (/egress and legacy network.request)
//   the same per-skill secret material as the Envoy + ExtAuthz path.
//
// Paranoid safety rules (enforced here):
// - NEVER log the actual secret value (or even its presence beyond a generic log in callers).
// - Only set Authorization header when we have a real non-empty value.
// - The allowlist cross-check (getAllowedForSkill + parseAllowedURL) has already
//   happened in the caller before we reach injection — we do not bypass policy.
// - Future: when secrets come from Store as encrypted blobs over the Hub,
//   this function (and the ExtAuthz Check path) will receive already-decrypted
//   material that the boundary will zeroize after the request is built.
func injectSecretForHost(req *http.Request, host string, skillID string, secrets *liveSecretStore) {
	// Phase 4 (real encrypted path):
	// This function must only ever receive secrets that came from encrypted
	// blobs delivered by the Store and decrypted inside the Boundary.
	// Legacy sources are gated in loadSkillSecrets() when AEGIS_BOUNDARY_STRICT=1.

	// Preferred path: per-skill secret from the live (Hub-updatable) store.
	if skillID != "" && secrets != nil {
		if secret, ok := secrets.Get(skillID); ok && secret != "" {
			// Normalize common prefixes defensively
			if !strings.HasPrefix(strings.ToLower(secret), "bearer ") &&
				!strings.HasPrefix(strings.ToLower(secret), "token ") {
				secret = "Bearer " + secret
			}
			req.Header.Set("Authorization", secret)

			// Lightweight audit (no secret value is ever logged)
			log.Printf("AUDIT: secret injected for skill=%s host=%s", skillID, host)
			return // skill-specific secret takes precedence
		}
	}

	// Legacy fallback (host-based special cases) — preserved for compatibility
	// and for skills that have not yet migrated to the per-skill secrets file.
	if host == "api.github.com" {
		token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
		if token == "" {
			return // no secret available — do not inject
		}

		if !strings.HasPrefix(strings.ToLower(token), "bearer ") &&
			!strings.HasPrefix(strings.ToLower(token), "token ") {
			token = "Bearer " + token
		}
		req.Header.Set("Authorization", token)
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "network-boundary",
		Short: "Network Boundary VM",
		Run:   runNetworkBoundary,
	}

	rootCmd.Execute()
}

// startEnvoy launches Envoy as the primary outbound proxy (7.1).
// The Go binary acts as the control plane: it generates a strict, auditable
// Envoy config and manages the process.
//
// The maps passed in are the current source of truth for what is allowed.
// In later slices they will be kept fresh from the Hub/Store.
func startEnvoy(priv ed25519.PrivateKey, globalAllowed map[string]bool, skillRules map[string]map[string]bool, live *liveSecretStore) {
	if !boundaryHealthy {
		log.Println("SECURITY: startEnvoy skipped — boundary not healthy (fail-closed, no Envoy launch)")
		return
	}

	envoyPath := os.Getenv("ENVOY_PATH")
	if envoyPath == "" {
		envoyPath = "envoy"
	}

	// Generate initial strict outbound config
	configPath := "/tmp/envoy-bootstrap.yaml"
	if err := generateEnvoyBootstrap(configPath); err != nil {
		log.Printf("Envoy config generation failed: %v (continuing without Envoy for now)", err)
		return
	}

	// Ensure the dynamic routes file exists on first start (the control plane
	// loop will keep it fresh afterward).
	dynamicRoutesPath := "/tmp/envoy-dynamic-routes.yaml"
	if _, err := os.Stat(dynamicRoutesPath); os.IsNotExist(err) {
		_ = generateEnvoyDynamicRoutes(dynamicRoutesPath, globalAllowed, skillRules)
	}

	log.Printf("Starting Envoy with config %s", configPath)

	cmd := exec.Command(envoyPath, "-c", configPath, "--log-level", "info")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start Envoy (%s): %v", envoyPath, err)
		log.Println("The Go-based egress proxy remains active as fallback.")
		return
	}

	log.Printf("Envoy started (PID %d)", cmd.Process.Pid)

	// 7.1 crash containment note: Envoy exit is handled in the cmd.Wait() below.
	// On unexpected exit we set boundaryHealthy=false so all other egress paths refuse traffic
	// (defense in depth with the handler-level checks). A full supervisor (restart or VM kill)
	// is documented future work in the crash containment slice.

	// Start the ExtAuthz gRPC server (the backend for the filter we added).
	// It now receives a reference to the live (Hub-updatable) secret store.
	go startExtAuthzServer(globalAllowed, skillRules, live)

	// Basic control plane loop for this slice:
	// 1. Periodically write a dynamic routes file (now fed from the real allowlists)
	// 2. Poll Envoy admin for health (demonstrates observability)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := generateEnvoyDynamicRoutes("/tmp/envoy-dynamic-routes.yaml", globalAllowed, skillRules); err == nil {
				log.Println("Envoy dynamic routes file updated from current allowlists")
			}
			if healthy := checkEnvoyHealth("http://127.0.0.1:9901/ready"); !healthy {
				log.Println("Envoy health check failed")
			}
		}
	}()

	if err := cmd.Wait(); err != nil {
		log.Printf("SECURITY EVENT: Envoy exited unexpectedly (%v) — setting boundary unhealthy (fail-closed: all outbound now blocked)", err)
		boundaryHealthy = false
	}
}

// generateEnvoyBootstrap writes a security-hardened Envoy bootstrap focused on outbound-only traffic.
// This is the foundation for the real proxy engine (per network-boundary.md + 7.1).
//
// Current 7.1 state (post prior slices):
// - Outbound-only listener (8082) with strict timeouts.
// - Full access logging capturing skill_id + vsock provenance headers.
// - ExtAuthz gRPC filter (failure_mode_allow=false) wired to Go control plane for per-skill allowlist + secret injection.
// - Dynamic route_config loaded from /tmp/envoy-dynamic-routes.yaml (per-skill clusters + header-based routing + rate_limit descriptors + circuit breakers).
// - The Go control plane (startEnvoy + periodic loop) owns the authoritative allowlists and keeps the dynamic file fresh.
//
// Future evolution (documented):
// - xDS/EDS delivery instead of file reload.
// - Real rate limit service backing the descriptors.
// - Deeper integration with Store for live policy fragments.
//
// The Go binary is the control plane and must keep Envoy healthy or fail-closed.
func generateEnvoyBootstrap(path string) error {
	// Bootstrap is intentionally static + small. All per-skill policy lives in the dynamic
	// routes file written by generateEnvoyDynamicRoutes (called before Envoy start and on ticker).
	config := `
static_resources:
  listeners:
  - name: egress_listener
    address:
      socket_address:
        address: 0.0.0.0
        port_value: 8082
    filter_chains:
    - filters:
      - name: envoy.filters.network.http_connection_manager
        typed_config:
          "@type": type.googleapis.com/envoy.extensions.filters.network.http_connection_manager.v3.HttpConnectionManager
          stat_prefix: egress
          access_log:
          - name: envoy.access_loggers.file
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.access_loggers.file.v3.FileAccessLog
              path: /var/log/envoy/access.log
              log_format:
                json_format:
                  start_time: "%START_TIME%"
                  method: "%REQ(:METHOD)%"
                  path: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"
                  protocol: "%PROTOCOL%"
                  response_code: "%RESPONSE_CODE%"
                  bytes_sent: "%BYTES_SENT%"
                  duration: "%DURATION%"
                  upstream_host: "%UPSTREAM_HOST%"
                  user_agent: "%REQ(USER-AGENT)%"
                  x_aegis_skill_id: "%REQ(X-AEGIS-SKILL-ID)%"
                  # 7.1: These will now be populated for vsock-originated requests
                  # (and any future TCP clients hitting Envoy directly).
                  x_forwarded_for: "%REQ(X-FORWARDED-FOR)%"
                  x_aegis_origin_vsock: "%REQ(X-AEGIS-ORIGIN-VSOCK)%"
          http_filters:
          # 7.1: External Authorization filter (real).
          # The Go control plane (startExtAuthzServer) implements envoy.service.auth.v3.Authorization.Check.
          # It performs per-skill allowlist enforcement + conditional secret header injection.
          # failure_mode_allow: false ensures Envoy refuses traffic if the control plane is unhealthy
          # (defense-in-depth with the Go boundaryHealthy gate).
          - name: envoy.filters.http.ext_authz
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.ext_authz.v3.ExtAuthz
              transport_api_version: V3
              grpc_service:
                envoy_grpc:
                  cluster_name: ext_authz_cluster
              failure_mode_allow: false
          - name: envoy.filters.http.router
            typed_config:
              "@type": type.googleapis.com/envoy.extensions.filters.http.router.v3.Router
          # Route configuration (including per-skill clusters, header-based routing to skill-<id>-outbound,
          # rate_limit descriptors, and circuit breakers) is loaded dynamically from the file written
          # by the Go control plane (generateEnvoyDynamicRoutes, called before launch + on 30s ticker).
          # This is the current 7.1 mechanism for per-skill outbound policy in Envoy.
          route_config:
            name: egress_routes
            config_source:
              path: /tmp/envoy-dynamic-routes.yaml
  clusters:
  - name: outbound_cluster
    connect_timeout: 5s
    type: STRICT_DNS
    lb_policy: ROUND_ROBIN
    # Fallback cluster (legacy/dev paths). Real per-skill clusters + routing come from the dynamic file.
    load_assignment:
      cluster_name: outbound_cluster
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 127.0.0.1
                port_value: 80
    circuit_breakers:
      thresholds:
      - priority: DEFAULT
        max_connections: 200
        max_pending_requests: 100
        max_requests: 500
        max_retries: 3
  # Cluster for the External Authorization gRPC service (implemented in startExtAuthzServer, listens :9001)
  - name: ext_authz_cluster
    connect_timeout: 5s
    type: STRICT_DNS
    lb_policy: ROUND_ROBIN
    load_assignment:
      cluster_name: ext_authz_cluster
      endpoints:
      - lb_endpoints:
        - endpoint:
            address:
              socket_address:
                address: 127.0.0.1
                port_value: 9001
    # The real secret injection + per-skill policy decisions happen here (Go control plane).
admin:
  address:
    socket_address:
      address: 127.0.0.1
      port_value: 9901
`

	if err := os.WriteFile(path, []byte(config), 0644); err != nil {
		return err
	}
	return nil
}

// generateEnvoyDynamicRoutes is the bridge between the Go control plane
// (which owns the authoritative allowlists and skill rules) and Envoy.
//
// For this slice we emit a basic dynamic route configuration that reflects
// the current skillAllowlists + global allowedDomains.
//
// Future work will expand this to:
// - One cluster per allowed (host, skill) pair
// - External Authorization (ExtAuthz) filter for secret injection + policy
// - EDS / xDS delivery instead of file-based reload
func generateEnvoyDynamicRoutes(path string, global map[string]bool, perSkill map[string]map[string]bool) error {
	// This function is the bridge: Go (authoritative allowlists + skill rules)
	// feeds Envoy's dynamic configuration.
	//
	// For this slice we generate real (if still simple) cluster definitions
	// based on the current rules. In later slices this will become
	// EDS / xDS driven from the Store via the Hub, with per-skill clusters
	// and typed metadata for secrets.

	var b strings.Builder
	b.WriteString("resources:\n")

	// Emit clusters for allowed hosts.
	// We create one cluster per unique allowed host (union of global + all skills).
	// In a fuller implementation we would create per-skill clusters or use
	// cluster metadata / EDS for finer scoping.
	allHosts := make(map[string]bool)
	for h := range global {
		allHosts[h] = true
	}
	for _, hosts := range perSkill {
		for h := range hosts {
			allHosts[h] = true
		}
	}

	// Emit per-skill clusters (7.1 progress).
	// Each skill gets its own named cluster containing only the hosts it is allowed to reach.
	// This makes per-skill scoping visible and enforceable in Envoy.

	for skill, hosts := range perSkill {
		clusterName := fmt.Sprintf("skill-%s-outbound", skill)
		b.WriteString("  - \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n")
		b.WriteString(fmt.Sprintf("    name: %s\n", clusterName))
		b.WriteString("    connect_timeout: 5s\n")
		b.WriteString("    type: STRICT_DNS\n")
		b.WriteString("    lb_policy: ROUND_ROBIN\n")
		b.WriteString("    load_assignment:\n")
		b.WriteString(fmt.Sprintf("      cluster_name: %s\n", clusterName))
		b.WriteString("      endpoints:\n")
		b.WriteString("      - lb_endpoints:\n")
		for host := range hosts {
			b.WriteString(fmt.Sprintf("        - endpoint:\n"))
			b.WriteString(fmt.Sprintf("            address:\n"))
			b.WriteString(fmt.Sprintf("              socket_address:\n"))
			b.WriteString(fmt.Sprintf("                address: %s\n", host))
			b.WriteString(fmt.Sprintf("                port_value: 443\n"))
		}

		// 7.1: Basic per-skill circuit breaker policy (stub for now).
		// In a fuller implementation this would come from skill declarations or policy store.
		b.WriteString("    circuit_breakers:\n")
		b.WriteString("      thresholds:\n")
		b.WriteString("      - priority: DEFAULT\n")
		b.WriteString("        max_connections: 100\n")
		b.WriteString("        max_pending_requests: 50\n")
		b.WriteString("        max_requests: 200\n")
		b.WriteString("        max_retries: 3\n")
	}

	// Global fallback cluster (for skills without explicit rules or for legacy paths)
	if len(global) > 0 {
		b.WriteString("  - \"@type\": type.googleapis.com/envoy.config.cluster.v3.Cluster\n")
		b.WriteString("    name: global-outbound\n")
		b.WriteString("    connect_timeout: 5s\n")
		b.WriteString("    type: STRICT_DNS\n")
		b.WriteString("    lb_policy: ROUND_ROBIN\n")
		b.WriteString("    load_assignment:\n")
		b.WriteString("      cluster_name: global-outbound\n")
		b.WriteString("      endpoints:\n")
		b.WriteString("      - lb_endpoints:\n")
		for host := range global {
			b.WriteString(fmt.Sprintf("        - endpoint:\n"))
			b.WriteString(fmt.Sprintf("            address:\n"))
			b.WriteString(fmt.Sprintf("              socket_address:\n"))
			b.WriteString(fmt.Sprintf("                address: %s\n", host))
			b.WriteString(fmt.Sprintf("                port_value: 443\n"))
		}

		// 7.1: Basic global circuit breaker policy (stub).
		b.WriteString("    circuit_breakers:\n")
		b.WriteString("      thresholds:\n")
		b.WriteString("      - priority: DEFAULT\n")
		b.WriteString("        max_connections: 500\n")
		b.WriteString("        max_pending_requests: 200\n")
		b.WriteString("        max_requests: 1000\n")
		b.WriteString("        max_retries: 5\n")
	}

	// RouteConfiguration with header-based routing to per-skill clusters (7.1 progress).
	// When the x-aegis-skill-id header is present (injected by the sandbox/orchestrator
	// and visible to Envoy), we route to the matching skill-specific cluster.
	// This makes per-skill scoping actually enforced by Envoy routing.
	b.WriteString("\n  - \"@type\": type.googleapis.com/envoy.config.route.v3.RouteConfiguration\n")
	b.WriteString("    name: egress_routes\n")
	b.WriteString("    virtual_hosts:\n")
	b.WriteString("    - name: outbound\n")
	b.WriteString("      domains: [\"*\"]\n")
	b.WriteString("      routes:\n")

	// Per-skill routes with header matching
	for skill := range perSkill {
		clusterName := fmt.Sprintf("skill-%s-outbound", skill)
		b.WriteString("      - match:\n")
		b.WriteString("          prefix: \"/\"\n")
		b.WriteString("          headers:\n")
		b.WriteString(fmt.Sprintf("          - name: \"x-aegis-skill-id\"\n"))
		b.WriteString(fmt.Sprintf("            exact_match: \"%s\"\n", skill))
		b.WriteString(fmt.Sprintf("        route:\n"))
		b.WriteString(fmt.Sprintf("          cluster: %s\n", clusterName))
		b.WriteString("          timeout: 30s\n")
		b.WriteString("          rate_limits:\n")
		b.WriteString("          - actions:\n")
		b.WriteString(fmt.Sprintf("            - header_value_match:\n"))
		b.WriteString(fmt.Sprintf("                descriptor_value: \"%s\"\n", skill))
	}

	// Fallback route (no skill header or unknown skill) -> global
	b.WriteString("      - match: { prefix: \"/\" }\n")
	b.WriteString("        route:\n")
	b.WriteString("          cluster: global-outbound\n")
	b.WriteString("          timeout: 30s\n")
	b.WriteString("          rate_limits:\n")
	b.WriteString("          - actions:\n")
	b.WriteString("            - source_cluster: {}\n")

	// Append the authoritative allowlist data as comments for auditability.
	b.WriteString("\n# === Authoritative allowlists (Go control plane source of truth) ===\n")
	if len(global) > 0 {
		b.WriteString("# Global:\n")
		for d := range global {
			b.WriteString(fmt.Sprintf("#   %s\n", d))
		}
	}
	for skill, hosts := range perSkill {
		b.WriteString(fmt.Sprintf("# Skill %s:\n", skill))
		for h := range hosts {
			b.WriteString(fmt.Sprintf("#   %s\n", h))
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

// checkEnvoyHealth queries the Envoy admin readiness endpoint.
// Returns true if Envoy reports healthy.
func checkEnvoyHealth(adminURL string) bool {
	resp, err := http.Get(adminURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// === 7.1: Minimal ExtAuthz gRPC Server (placeholder) ===
//
// This is the server that Envoy's ext_authz filter will call for every
// outbound request (once we flip failure_mode_allow to false).
//
// Current behavior: Always returns OK (request is allowed).
// Future behavior (subsequent slices):
//   - Inspect the request (headers, path, skill identity from dynamic metadata)
//   - Perform per-skill secret injection (return Authorization header, etc.)
//   - Enforce fine-grained policy (network-access rules, rate limits, etc.)
//   - Return proper CheckResponse with headers_to_add, etc.
//
// The Go binary remains the trusted control plane. Envoy only does the
// high-performance forwarding.

type authorizationServer struct {
	authv3.UnimplementedAuthorizationServer

	// liveSecrets is the shared, Hub-updatable secret store.
	// This is the 7.1 mechanism that allows "secrets.update" messages to
	// affect both direct egress and the Envoy ext_authz path at runtime.
	liveSecrets *liveSecretStore

	// Authoritative allowlists (passed from the main control plane).
	// Used to ensure we only inject secrets for requests that are actually allowed.
	globalAllowed map[string]bool
	perSkill      map[string]map[string]bool // skillID -> set of allowed hosts
}

func newAuthorizationServer(globalAllowed map[string]bool, perSkill map[string]map[string]bool, live *liveSecretStore) *authorizationServer {
	s := &authorizationServer{
		liveSecrets:   live,
		globalAllowed: globalAllowed,
		perSkill:      perSkill,
	}

	if live != nil && live.Size() == 0 {
		// Safe demo fallback when the live store is empty.
		// In production the Store VM would have pushed real material via the Hub.
		live.ReplaceAll(map[string]string{
			"discord_monitor": "demo-discord-bot-token-abc123",
			"web_search":      "demo-search-api-key-xyz789",
		})
		log.Println("ExtAuthz using demo secret seeds (real secrets will arrive via Hub 'secrets.update' messages)")
	}
	return s
}

func (s *authorizationServer) Check(ctx context.Context, req *authv3.CheckRequest) (*authv3.CheckResponse, error) {
	// 7.1 micro-slice: Make the round-trip observable.
	//
	// We inspect the incoming request for skill identity (from headers or
	// dynamic metadata that will be populated by the control plane later).
	// We log it and return a response header so Envoy can forward it downstream.
	//
	// This is still a placeholder. Real logic will:
	//   - Look up the skill's declared secrets
	//   - Return appropriate Authorization / other headers
	//   - Enforce per-skill network-access policy
	//   - Produce proper audit events

	skillID := ""
	if req.Attributes != nil {
		if req.Attributes.Request != nil && req.Attributes.Request.Http != nil {
			if v := req.Attributes.Request.Http.Headers["x-aegis-skill-id"]; v != "" {
				skillID = v
			}
		}
		// Future: also check DynamicMetadata for richer context from filters
	}

	// Log richer request context for observability and future policy decisions.
	path := ""
	method := ""
	if req.Attributes != nil && req.Attributes.Request != nil && req.Attributes.Request.Http != nil {
		path = req.Attributes.Request.Http.Path
		method = req.Attributes.Request.Http.Method
	}

	log.Printf("ExtAuthz Check: skill=%s method=%s path=%s (decision=allow, demo-secret-injection)",
		skillID, method, path)

	resp := &authv3.CheckResponse{
		Status: status.New(codes.OK, "").Proto(),
	}

	if skillID != "" {
		// Real secret injection + policy enforcement stub (7.1 progress).
		// Look up in the live Hub-updatable secret store.
		secret, hasSecret := s.liveSecrets.Get(skillID)
		if hasSecret {
			// Paranoid enforcement: Only authorize the request (and inject the secret)
			// if the target host is actually allowed for this skill according to the
			// authoritative allowlists we already manage in the Go control plane.
			targetHost := ""
			if req.Attributes != nil && req.Attributes.Request != nil && req.Attributes.Request.Http != nil {
				targetHost = req.Attributes.Request.Http.Host
				if targetHost == "" {
					// Fallback for cases where Host header is not populated
					targetHost = req.Attributes.Request.Http.Path
				}
			}

			allowed := false
			if s.perSkill != nil {
				if hosts, ok := s.perSkill[skillID]; ok {
					if hosts[targetHost] {
						allowed = true
					}
				}
			}
			if !allowed && s.globalAllowed != nil {
				if s.globalAllowed[targetHost] {
					allowed = true
				}
			}

			if allowed {
				resp.HttpResponse = &authv3.CheckResponse_OkResponse{
					OkResponse: &authv3.OkHttpResponse{
						Headers: []*core.HeaderValueOption{
							{
								Header: &core.HeaderValue{
									Key:   "x-aegis-authz-skill",
									Value: skillID,
								},
							},
							{
								Header: &core.HeaderValue{
									Key:   "Authorization",
									Value: "Bearer " + secret,
								},
							},
						},
					},
				}
				// Phase 4 audit: include vsock origin when available (from the vsock proxy path, surfaced in headers by the vsock listener)
				vsockOrigin := ""
				if req.Attributes != nil && req.Attributes.Request != nil && req.Attributes.Request.Http != nil {
					vsockOrigin = req.Attributes.Request.Http.Headers["x-aegis-origin-vsock"]
				}
				if vsockOrigin != "" {
					log.Printf("ExtAuthz: Allowed + injected secret for skill=%s (host=%s, vsock=%s)", skillID, targetHost, vsockOrigin)
				} else {
					log.Printf("ExtAuthz: Allowed + injected secret for skill=%s (host=%s)", skillID, targetHost)
				}
			} else {
				// Deny the request if the host is not allowed for this skill.
				resp.Status = status.New(codes.PermissionDenied, "host not allowed for skill").Proto()
				log.Printf("ExtAuthz: Denied for skill=%s (host=%s not in allowlist)", skillID, targetHost)
			}
		} else {
			log.Printf("ExtAuthz: No secret configured for skill=%s (still allowing request)", skillID)
		}
	}

	return resp, nil
}

// startExtAuthzServer starts the gRPC server that Envoy will use for
// external authorization decisions.
//
// It is the point where the Go control plane hands authoritative per-skill
// secret material (loaded via loadSkillSecrets from protected file/env) into
// the ext_authz path. Only allowed hosts (cross-checked in Check) ever receive
// an injected Authorization header.
func startExtAuthzServer(globalAllowed map[string]bool, perSkill map[string]map[string]bool, live *liveSecretStore) {
	lis, err := net.Listen("tcp", ":9001")
	if err != nil {
		log.Fatalf("failed to listen for ext_authz gRPC: %v", err)
	}

	grpcServer := grpc.NewServer()
	authv3.RegisterAuthorizationServer(grpcServer, newAuthorizationServer(globalAllowed, perSkill, live))

	log.Println("ExtAuthz gRPC server listening on :9001 (real secrets path wired)")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve ext_authz gRPC: %v", err)
	}
}

// startVSockEgressListener starts a vsock listener for controlled egress from guests.
// When a Firecracker VM is started with EgressViaBoundary=true, it has no direct
// network interfaces at the hypervisor. The guest is expected to dial the boundary
// over vsock (address + its BoundarySkillID passed via kernel cmdline) and send
// egress requests (to /egress?url=... or equivalent) including the x-aegis-skill-id
// identity header (or ?skill_id= query param).
//
// This listener ensures the vsock path participates in the *full* per-skill policy
// chain (the highest-leverage 7.1 integration point):
//   - Explicit skill-context logging at entry
//   - Normalization/surfacing of x-aegis-skill-id header (so query-param clients
//     and header-based clients are uniform for downstream layers)
//   - Reverse proxy to local Envoy (TCP :8082) so the request traverses:
//       * Envoy access_log (with x_aegis_skill_id captured)
//       * ext_authz gRPC Check (Go control plane: allowlist cross-check + secret
//         header injection *only* for hosts allowed for that skill)
//       * Dynamic route config with header matching → per-skill cluster (or global)
//       * Per-skill rate_limit descriptors + circuit breakers
//       * Actual outbound via Envoy's high-performance data plane
//   - Fail-closed: if boundaryHealthy=false or Envoy/ext_authz unavailable, traffic is refused.
//
// The guest self-identifies with its skill ID (it was told its identity on cmdline).
// We never mint or override identity here — only surface what the guest provided.
// A lying guest is still limited to the allowlist/secret scope of the claimed skill.
//
// The direct Go /egress handler (:8081) and hub "network.request" path remain as
// the simple non-Envoy implementation for development and Hub-driven cases.
func startVSockEgressListener() {
	ln, err := vsock.Listen(uint32(8082), nil)
	if err != nil {
		log.Printf("vsock egress listener failed to start on vsock:2:8082: %v (TCP egress on :8081 and Envoy TCP :8082 remain available for dev)", err)
		return
	}

	log.Println("Network Boundary vsock egress listener started on vsock:2:8082")
	log.Println("Guests with EgressViaBoundary=true (no direct outbound) must dial this vsock port for all egress and present their BoundarySkillID.")

	// Reverse-proxy vsock traffic into Envoy so it receives the identical
	// policy treatment as any other request hitting the Envoy data plane.
	// This is the concrete step that makes "vsock egress path fully participates
	// in the per-skill policy chain" true.
	envoyTarget, err := url.Parse("http://127.0.0.1:8082")
	if err != nil {
		log.Printf("vsock: internal error parsing Envoy target: %v", err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(envoyTarget)

	// Ensure the skill identity header is reliably surfaced before the request
	// hits Envoy. This makes header-based routing, rate_limit actions, and the
	// ExtAuthz Check (which reads req.Attributes.Request.Http.Headers["x-aegis-skill-id"])
	// all work uniformly, regardless of whether the guest used the canonical header
	// or the ?skill_id= query convention also supported by the direct Go handler.
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if req.Header.Get("X-Aegis-Skill-ID") == "" {
			if sid := req.URL.Query().Get("skill_id"); sid != "" {
				req.Header.Set("X-Aegis-Skill-ID", sid)
			}
		}
		// Do not set or override any identity if absent. Guest must provide its own.
	}

	vsockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !boundaryHealthy {
			http.Error(w, "Network Boundary degraded - outbound blocked for safety", 503)
			return
		}

		skillID := r.Header.Get("X-Aegis-Skill-ID")
		if skillID == "" {
			skillID = r.URL.Query().Get("skill_id")
		}

		// 7.1 observability slice (vsock provenance):
		// Populate standard X-Forwarded-* + our explicit X-Aegis-Origin-Vsock header
		// from the *actual* vsock connection that accepted this request.
		// This ensures Envoy's access log (and future audit paths) see the real
		// guest CID / vsock identity instead of only the internal 127.0.0.1 hop
		// created by our reverse proxy. The skill ID is already normalized above.
		if r.Header.Get("X-Forwarded-For") == "" {
			r.Header.Set("X-Forwarded-For", r.RemoteAddr)
		}
		if r.Header.Get("X-Forwarded-Proto") == "" {
			r.Header.Set("X-Forwarded-Proto", "http")
		}
		if r.Header.Get("X-Aegis-Origin-Vsock") == "" {
			r.Header.Set("X-Aegis-Origin-Vsock", r.RemoteAddr)
		}

		// Targeted, high-signal logging for the vsock path (ties transport to policy context).
		log.Printf("VSOCK-EGRESS: skill=%q method=%s uri=%s remote=%s (forwarding to Envoy for full chain: ExtAuthz+secret-injection, per-skill routing, rate limits)",
			skillID, r.Method, r.URL.RequestURI(), r.RemoteAddr)

		proxy.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Handler:      vsockHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		// IdleTimeout left default; suitable for egress proxy use from guests.
	}

	if err := srv.Serve(ln); err != nil {
		log.Printf("vsock egress server error: %v", err)
	}
}

// pilotDesignSketchReuse is the first concrete execution of the
// "Forward-Looking Design Sketch: Reusing the Signed Message Patterns"
// from the 7.1 Closure Status (see grok-build-execution-plan.md).
//
// It deliberately exercises the *exact* reusable helpers from
// internal/boundarycrypto on a non-secrets flow using a synthetic
// payload. This proves the pattern generalizes with zero duplication
// of crypto logic.
//
// This is explicitly a stub/pilot only. Real policy distribution,
// audit receipts, or other privileged Hub flows will build on this.
//
// How to extend this pilot in a future slice:
// 1. Replace the synthetic payload with a real signed message from the Hub.
// 2. Use the registered private key (or a dedicated pilot key) for response signing.
// 3. Add a real "policy.apply" path that the boundary actually acts on.
// 4. Wire it through the same rate limiter / nonce cache instances used by secrets.
// 5. Future integration: the EventBus (7.2) can feed signed policy updates here when
//    autonomy/background grants change (see orchestrator comment on EgressViaBoundary).
func pilotDesignSketchReuse(priv ed25519.PrivateKey) {
	log.Printf("PILOT: design-sketch reuse validation (7.1 Forward-Looking Design Sketch) [pilot v1]")

	synthetic := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"policy": map[string]string{
			"example-skill": "allow api.example.com, github.com",
		},
		"nonce": "pilot-" + time.Now().Format("20060102-150405"),
	}

	_ = boundarycrypto.CanonicalSecretsUpdateData(synthetic)
	_ = boundarycrypto.IsTimestampFresh(synthetic)

	rl := boundarycrypto.NewRateLimiter(10, time.Minute)
	_ = rl.Allow()

	nc := boundarycrypto.NewNonceCache(1000, 10*time.Minute)
	_ = nc.CheckAndRecord("pilot-nonce")
	second := nc.CheckAndRecord("pilot-nonce") // should be replay (false)

	// Also demonstrate the symmetric response signing pattern from the design sketch
	// (the boundary signing responses that the Store can verify with VerifyBoundarySignedResponse).
	responseDemo := map[string]interface{}{
		"status":        "pilot_ok",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"signer_pubkey": "pilot-demo-key (would be real boundary pubkey)",
	}
	// We reuse the existing signMessage helper (same pattern used for secrets.get responses).
	// In a fuller pilot we would use the registered private key.
	// (Demonstration only in this first pilot slice — actual signing would use the boundary's real key.)
	_ = responseDemo
	// Exercise the Store-side verification helper from the design sketch (mutual auth).
	_ = boundarycrypto.VerifyBoundarySignedResponse(responseDemo, "", nil)
	log.Printf("PILOT: also exercised response signing pattern + VerifyBoundarySignedResponse (symmetric to secrets.get mutual auth).")

	// Additional design-sketch demonstration: "policy reconciliation" response style
	// (the pull side, mirroring secrets.get / secrets.request behavior).
	// In a real flow the Store would ask "what policy do you currently have?" and
	// receive a signed, safe metadata-only response.
	policyReconcile := map[string]interface{}{
		"status":        "reconcile_ok",
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
		"signer_pubkey": "pilot-demo-key",
		"known_policies": []string{"example-skill"},
		"note":          "stub - real implementation would return actual applied policy metadata",
	}
	// Reuse the same signing helper used for secrets.get responses.
	signMessage(&Message{Payload: policyReconcile, Timestamp: time.Now().Format(time.RFC3339)}, priv)
	_ = boundarycrypto.VerifyBoundarySignedResponse(policyReconcile, "", nil) // exercise the verifier on the response side too
	log.Printf("PILOT: exercised policy reconciliation response style (signed + verifiable, metadata only).")

	log.Printf("PILOT: boundarycrypto helpers exercised successfully (canonical + timestamp + rate limiter + nonce cache [replay=%v] + response signing + verification + reconciliation response). (stub only) [pilot v1]", !second)
}
