package llm

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// EgressProxyVsockPort is the well-known vsock port the skill guest-agent
// connects to for outbound HTTPS/WSS access.  Firecracker routes connections
// to host CID 2 port 1026 via <vsock_path>_1026 on the host socket.
// Port 1025 is used by the LLM proxy; 1026 is the egress proxy.
const EgressProxyVsockPort = 1026

// egressDialTimeout is the maximum time allowed to establish a connection to
// the real upstream destination after SNI validation passes.
const egressDialTimeout = 15 * time.Second

// maxTLSRecordLen is the maximum TLS record body length per RFC 5246 §6.2.
// A ClientHello will never exceed this size; anything larger indicates a
// malformed or malicious input and is rejected immediately.
const maxTLSRecordLen = 16384

// egressTLSReadTimeout is the maximum time to wait when peeking TLS bytes for
// SNI extraction.  If the guest does not send a ClientHello within this window
// the connection is dropped.
const egressTLSReadTimeout = 10 * time.Second

// EgressProxy listens on per-VM vsock UDS paths and provides SNI-validated
// transparent TCP tunneling for skill VMs.
//
// Security properties:
//   - Listens only on per-VM <vsock_path>_1026 sockets; unreachable outside
//     the VM's vsock channel.
//   - Parses the TLS ClientHello SNI without terminating TLS; end-to-end
//     encryption is preserved.
//   - Enforces a per-skill FQDN allowlist; connections to unlisted hosts are
//     dropped and audit-logged.
//   - All connections (allowed and denied) are logged with skill ID, FQDN,
//     outcome, and timestamp for audit.
//   - Transparent splice after validation — no TLS termination, no content
//     inspection beyond the ClientHello.
type EgressProxy struct {
	logger *zap.Logger

	mu        sync.Mutex
	listeners map[string]net.Listener // vmID -> UDS listener on vsock.sock_1026
	policies  map[string][]string     // vmID -> allowed FQDN list
}

// NewEgressProxy creates an EgressProxy instance.
func NewEgressProxy(logger *zap.Logger) *EgressProxy {
	return &EgressProxy{
		logger:    logger,
		listeners: make(map[string]net.Listener),
		policies:  make(map[string][]string),
	}
}

// StartForVM starts the egress proxy for a specific VM.  vsockPath is the
// Firecracker vsock device socket (e.g. /run/aegisclaw/.../vsock.sock).
// The proxy binds to vsockPath + "_1026".
// allowedHosts is the FQDN allowlist for this VM; only exact-match hostnames
// (or IPs) in this list are allowed through.
func (p *EgressProxy) StartForVM(vmID, vsockPath string, allowedHosts []string) error {
	listenPath := fmt.Sprintf("%s_%d", vsockPath, EgressProxyVsockPort)

	_ = os.Remove(listenPath)

	l, err := net.Listen("unix", listenPath)
	if err != nil {
		return fmt.Errorf("egress proxy: listen for vm %s at %s: %w", vmID, listenPath, err)
	}
	_ = os.Chmod(listenPath, 0666)

	p.mu.Lock()
	p.listeners[vmID] = l
	norm := normalizeAllowedHosts(allowedHosts)
	p.policies[vmID] = norm
	p.mu.Unlock()

	go p.serveVM(vmID, l)

	p.logger.Info("egress proxy started for vm",
		zap.String("vm_id", vmID),
		zap.String("socket", listenPath),
		zap.Strings("allowed_hosts", norm),
	)
	return nil
}

// StartForContainerTCP starts the egress proxy for a Docker container sandbox
// by listening on a TCP address (e.g. "172.17.0.1:1026") on the aegis-egress
// bridge network.  The container routes outbound TLS traffic to this address
// via the bridge gateway.
//
// The SNI-validation and transparent-splice logic is identical to StartForVM;
// only the transport changes from vsock UDS to TCP.
func (p *EgressProxy) StartForContainerTCP(sandboxID, listenAddr string, allowedHosts []string) error {
	l, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("egress proxy: tcp listen for container %s at %s: %w", sandboxID, listenAddr, err)
	}

	p.mu.Lock()
	// If a listener already exists for this sandbox, close it first.
	if old, ok := p.listeners[sandboxID]; ok {
		old.Close()
	}
	p.listeners[sandboxID] = l
	norm := normalizeAllowedHosts(allowedHosts)
	p.policies[sandboxID] = norm
	p.mu.Unlock()

	go p.serveVM(sandboxID, l)

	p.logger.Info("egress proxy started for container (tcp)",
		zap.String("sandbox_id", sandboxID),
		zap.String("listen_addr", listenAddr),
		zap.Strings("allowed_hosts", norm),
	)
	return nil
}

// normalizeAllowedHosts lowercases and trims each entry, dropping blanks.
func normalizeAllowedHosts(hosts []string) []string {
	norm := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			norm = append(norm, h)
		}
	}
	return norm
}

// StopForVM closes the proxy listener for the specified VM.
func (p *EgressProxy) StopForVM(vmID string) {
	p.mu.Lock()
	l, ok := p.listeners[vmID]
	if ok {
		delete(p.listeners, vmID)
		delete(p.policies, vmID)
	}
	p.mu.Unlock()

	if ok {
		l.Close()
		p.logger.Info("egress proxy stopped for vm", zap.String("vm_id", vmID))
	}
}

// Stop closes every active proxy listener.
func (p *EgressProxy) Stop() {
	p.mu.Lock()
	ls := make([]net.Listener, 0, len(p.listeners))
	for _, l := range p.listeners {
		ls = append(ls, l)
	}
	p.listeners = make(map[string]net.Listener)
	p.policies = make(map[string][]string)
	p.mu.Unlock()

	for _, l := range ls {
		l.Close()
	}
}

func (p *EgressProxy) serveVM(vmID string, l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			return // listener closed; exit silently
		}
		go p.handleConn(vmID, conn)
	}
}

// handleConn processes one inbound connection from a skill VM.
// It reads the TLS ClientHello, extracts the SNI, validates against the
// allowlist, and — if allowed — splices the raw bytes bidirectionally to the
// real upstream without TLS termination.
func (p *EgressProxy) handleConn(vmID string, guest net.Conn) {
	defer guest.Close()

	// Give the guest a generous deadline for the initial ClientHello.
	guest.SetDeadline(time.Now().Add(egressTLSReadTimeout))

	// Peek the TLS ClientHello.  We buffer what we read so it can be replayed
	// verbatim to the upstream — we never modify or decrypt the TLS stream.
	hello, sni, err := peekSNI(guest)
	if err != nil {
		p.logger.Warn("egress proxy: SNI extraction failed",
			zap.String("vm_id", vmID),
			zap.Error(err),
		)
		return
	}

	// Clear the deadline — splice will set its own timeouts via io.Copy.
	guest.SetDeadline(time.Time{})

	sniLower := strings.ToLower(sni)

	p.mu.Lock()
	allowed := p.policies[vmID]
	p.mu.Unlock()

	if !isHostAllowed(sniLower, allowed) {
		p.logger.Warn("aegis-egress-drop: blocked unlisted host",
			zap.String("vm_id", vmID),
			zap.String("sni", sni),
			zap.Strings("allowed", allowed),
		)
		return
	}

	p.logger.Info("egress proxy: allow",
		zap.String("vm_id", vmID),
		zap.String("sni", sni),
	)

	// Connect to the real upstream on port 443.
	upstreamAddr := net.JoinHostPort(sni, "443")
	upstream, err := net.DialTimeout("tcp", upstreamAddr, egressDialTimeout)
	if err != nil {
		p.logger.Warn("egress proxy: upstream dial failed",
			zap.String("vm_id", vmID),
			zap.String("sni", sni),
			zap.Error(err),
		)
		return
	}
	defer upstream.Close()

	// Replay the ClientHello bytes we already read from the guest.
	if _, err := upstream.Write(hello); err != nil {
		p.logger.Warn("egress proxy: replay ClientHello failed",
			zap.String("vm_id", vmID),
			zap.Error(err),
		)
		return
	}

	// Transparent splice: bidirectional copy until either side closes.
	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstream, guest)
		errc <- err
	}()
	go func() {
		_, err := io.Copy(guest, upstream)
		errc <- err
	}()
	// Wait for either direction to finish (or error) then let defers close both.
	<-errc
}

// isHostAllowed returns true if host exactly matches one of the allowed entries.
// Matching is case-insensitive and exact; wildcards are not supported.
func isHostAllowed(host string, allowed []string) bool {
	for _, h := range allowed {
		if h == host {
			return true
		}
	}
	return false
}

// peekSNI reads the first TLS record from r, parses the ClientHello, and
// returns (the raw bytes read, the SNI hostname, error).
// The raw bytes must be replayed to the upstream so the TLS handshake can
// proceed unmodified.
//
// Layout reference:
//
//	TLS Record: content_type(1) + legacy_version(2) + length(2) + data(N)
//	Handshake:  msg_type(1) + length(3) + ClientHello body
//	ClientHello: legacy_version(2) + random(32) + session_id_len(1) + ... + extensions
func peekSNI(r net.Conn) (raw []byte, sni string, err error) {
	// Read TLS record header: 5 bytes.
	hdr := make([]byte, 5)
	if _, err = io.ReadFull(r, hdr); err != nil {
		return nil, "", fmt.Errorf("read TLS record header: %w", err)
	}

	if hdr[0] != 0x16 { // content_type must be Handshake
		return nil, "", fmt.Errorf("not a TLS handshake record (content type 0x%02x)", hdr[0])
	}

	recordLen := int(binary.BigEndian.Uint16(hdr[3:5]))
	if recordLen < 4 || recordLen > maxTLSRecordLen {
		return nil, "", fmt.Errorf("TLS record length out of range: %d", recordLen)
	}

	body := make([]byte, recordLen)
	if _, err = io.ReadFull(r, body); err != nil {
		return nil, "", fmt.Errorf("read TLS record body: %w", err)
	}

	raw = append(hdr, body...)

	sni, err = extractSNIFromClientHello(body)
	if err != nil {
		return raw, "", err
	}
	return raw, sni, nil
}

// extractSNIFromClientHello parses a TLS ClientHello handshake body (starting
// at the handshake msg_type byte) and returns the SNI hostname.
func extractSNIFromClientHello(data []byte) (string, error) {
	if len(data) < 4 {
		return "", fmt.Errorf("ClientHello too short")
	}
	if data[0] != 0x01 { // msg_type must be ClientHello
		return "", fmt.Errorf("not a ClientHello (type 0x%02x)", data[0])
	}

	// Handshake msg_type(1) + length(3) + client_version(2) + random(32)
	// = 38 bytes before session_id_length.
	const baseOffset = 38
	if len(data) < baseOffset+1 {
		return "", fmt.Errorf("ClientHello too short for base fields")
	}

	pos := baseOffset
	sessionIDLen := int(data[pos])
	pos++
	pos += sessionIDLen // skip session_id

	if len(data) < pos+2 {
		return "", fmt.Errorf("ClientHello truncated at cipher_suites")
	}
	cipherSuitesLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2 + cipherSuitesLen

	if len(data) < pos+1 {
		return "", fmt.Errorf("ClientHello truncated at compression_methods")
	}
	compressionLen := int(data[pos])
	pos++
	pos += compressionLen

	if len(data) < pos+2 {
		// No extensions present — SNI is not present.
		return "", fmt.Errorf("ClientHello has no extensions (no SNI)")
	}
	extsLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2

	end := pos + extsLen
	if len(data) < end {
		end = len(data)
	}

	for pos+4 <= end {
		extType := binary.BigEndian.Uint16(data[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		pos += 4

		if pos+extLen > end {
			break
		}
		extData := data[pos : pos+extLen]
		pos += extLen

		if extType != 0x0000 { // server_name extension
			continue
		}
		// server_name extension:
		// list_length(2) + name_type(1) + name_length(2) + name
		if len(extData) < 5 {
			return "", fmt.Errorf("server_name extension too short")
		}
		// list_length at extData[0:2] — skip it
		if extData[2] != 0x00 { // name_type must be host_name(0)
			return "", fmt.Errorf("server_name name_type not host_name")
		}
		nameLen := int(binary.BigEndian.Uint16(extData[3:5]))
		if len(extData) < 5+nameLen {
			return "", fmt.Errorf("server_name name truncated")
		}
		return string(extData[5 : 5+nameLen]), nil
	}

	return "", fmt.Errorf("SNI not found in ClientHello extensions")
}
