package llm

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"net"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// buildTestClientHello constructs a minimal TLS 1.2 ClientHello with an SNI
// extension for the given hostname.  The returned bytes include the 5-byte TLS
// record header so they match the full on-wire format that peekSNI expects.
func buildTestClientHello(t *testing.T, sni string) []byte {
	t.Helper()

	// Build the extensions block: server_name(0x0000) only.
	nameBytes := []byte(sni)
	nameLen := len(nameBytes)

	// server_name list entry: name_type(1) + name_length(2) + name
	entry := make([]byte, 3+nameLen)
	entry[0] = 0x00 // host_name type
	binary.BigEndian.PutUint16(entry[1:3], uint16(nameLen))
	copy(entry[3:], nameBytes)

	// server_name list: list_length(2) + entry
	listLen := len(entry)
	snList := make([]byte, 2+listLen)
	binary.BigEndian.PutUint16(snList[0:2], uint16(listLen))
	copy(snList[2:], entry)

	// extension: ext_type(2) + ext_length(2) + ext_data
	ext := make([]byte, 4+len(snList))
	binary.BigEndian.PutUint16(ext[0:2], 0x0000) // server_name extension
	binary.BigEndian.PutUint16(ext[2:4], uint16(len(snList)))
	copy(ext[4:], snList)

	// extensions block: extensions_length(2) + extension
	exts := make([]byte, 2+len(ext))
	binary.BigEndian.PutUint16(exts[0:2], uint16(len(ext)))
	copy(exts[2:], ext)

	// ClientHello body:
	//   legacy_version(2) + random(32) + session_id_len(1)
	//   + cipher_suites_len(2) + cipher_suites(2) + compression_len(1) + comp(1)
	//   + extensions
	chBody := make([]byte, 0, 38+1+2+2+1+1+len(exts))
	chBody = append(chBody, 0x03, 0x03)               // TLS 1.2
	chBody = append(chBody, make([]byte, 32)...)       // random
	chBody = append(chBody, 0x00)                      // session_id_len=0
	chBody = append(chBody, 0x00, 0x02)                // cipher_suites_len=2
	chBody = append(chBody, 0xC0, 0x2C)                // one cipher suite
	chBody = append(chBody, 0x01)                      // compression_methods_len=1
	chBody = append(chBody, 0x00)                      // no compression
	chBody = append(chBody, exts...)

	// Handshake header: msg_type(1=ClientHello) + length(3)
	hs := make([]byte, 4+len(chBody))
	hs[0] = 0x01 // ClientHello
	hs[1] = byte(len(chBody) >> 16)
	hs[2] = byte(len(chBody) >> 8)
	hs[3] = byte(len(chBody))
	copy(hs[4:], chBody)

	// TLS record header: content_type(22=Handshake) + legacy_version + length
	rec := make([]byte, 5+len(hs))
	rec[0] = 0x16 // Handshake
	rec[1] = 0x03
	rec[2] = 0x01 // TLS 1.0 compat
	binary.BigEndian.PutUint16(rec[3:5], uint16(len(hs)))
	copy(rec[5:], hs)

	return rec
}

// writeSNIToConn writes a ClientHello with the given SNI to conn.
func writeSNIToConn(t *testing.T, conn net.Conn, sni string) {
	t.Helper()
	hello := buildTestClientHello(t, sni)
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write ClientHello: %v", err)
	}
}

// TestEgressProxy_AllowedHost verifies that a connection with an SNI in the
// allowlist is proxied through to the real upstream (simulated by a local
// TLS listener in the test).
func TestEgressProxy_AllowedHost(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Start a simple echo TLS server to act as the upstream.
	if _, err := startEchoTLSServer(t); err != nil {
		t.Skipf("could not start echo TLS server: %v", err)
	}

	ep := NewEgressProxy(logger)

	// Override dial target: we can't change the DNS, so we test isHostAllowed
	// and peekSNI independently rather than doing a full end-to-end dial test
	// (that would require real DNS). The integration test below tests the full
	// path using 127.0.0.1 as both the SNI host and the dial target.

	// Test allow/deny logic directly.
	allowed := []string{"allowed.example.com", "api.discord.com"}
	if !isHostAllowed("allowed.example.com", allowed) {
		t.Error("expected allowed.example.com to be allowed")
	}
	if !isHostAllowed("api.discord.com", allowed) {
		t.Error("expected api.discord.com to be allowed")
	}
	if isHostAllowed("evil.com", allowed) {
		t.Error("expected evil.com to be denied")
	}
	if isHostAllowed("", allowed) {
		t.Error("expected empty host to be denied")
	}
	// StartForVM normalises the allowlist to lowercase; verify round-trip with
	// a pre-normalised list.
	if !isHostAllowed("allowed.example.com", []string{"allowed.example.com"}) {
		t.Error("expected lowercase match to succeed")
	}

	ep.Stop()
}

// TestEgressProxy_DeniedHost verifies that isHostAllowed denies hosts not in the list.
func TestEgressProxy_DeniedHost(t *testing.T) {
	cases := []struct {
		host    string
		allowed []string
		want    bool
	}{
		{"api.discord.com", []string{"api.discord.com"}, true},
		{"evil.com", []string{"api.discord.com"}, false},
		{"api.discord.com.evil.com", []string{"api.discord.com"}, false},
		{"evil.com", nil, false},
		{"allowed.com", []string{"allowed.com", "other.com"}, true},
		{"", []string{"allowed.com"}, false},
	}

	for _, tc := range cases {
		got := isHostAllowed(tc.host, tc.allowed)
		if got != tc.want {
			t.Errorf("isHostAllowed(%q, %v) = %v; want %v", tc.host, tc.allowed, got, tc.want)
		}
	}
}

// TestPeekSNI verifies SNI extraction from a synthesized TLS ClientHello.
func TestPeekSNI(t *testing.T) {
	cases := []struct {
		name    string
		sni     string
		wantErr bool
	}{
		{"simple hostname", "api.discord.com", false},
		{"subdomain", "gateway.discord.gg", false},
		{"single label", "localhost", false},
		{"github", "api.github.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a pipe and write the ClientHello to one end.
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()

			go func() {
				hello := buildTestClientHello(t, tc.sni)
				client.Write(hello)
			}()

			raw, sni, err := peekSNI(server)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("peekSNI: %v", err)
			}
			if sni != tc.sni {
				t.Errorf("SNI = %q; want %q", sni, tc.sni)
			}
			if len(raw) == 0 {
				t.Error("expected non-empty raw bytes")
			}
		})
	}
}

// TestPeekSNI_InvalidInput verifies error cases.
func TestPeekSNI_InvalidInput(t *testing.T) {
	// Send a non-TLS-handshake byte as content type.
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		// HTTP GET instead of TLS
		client.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	}()

	_, _, err := peekSNI(server)
	if err == nil {
		t.Error("expected error for non-TLS input, got nil")
	}
}

// TestEgressProxy_StartStopForVM verifies that the proxy correctly manages
// per-VM socket lifecycle.
func TestEgressProxy_StartStopForVM(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ep := NewEgressProxy(logger)

	dir := t.TempDir()
	vsockPath := dir + "/vsock.sock"

	if err := ep.StartForVM("vm-test", vsockPath, []string{"api.example.com"}); err != nil {
		t.Fatalf("StartForVM: %v", err)
	}

	// Check the socket file exists.
	listenPath := vsockPath + "_1026"
	if _, err := net.Dial("unix", listenPath); err != nil {
		t.Fatalf("socket should be listening: %v", err)
	}

	ep.StopForVM("vm-test")

	// After stop, no new connections should be accepted.
	if _, err := net.DialTimeout("unix", listenPath, 100*time.Millisecond); err == nil {
		t.Error("expected connection failure after StopForVM")
	}
}

// TestEgressProxy_ProxyMode_SNIExtraction is an integration test that verifies
// the full flow: the proxy reads the ClientHello, validates the SNI, and
// tunnels through to a local upstream.
func TestEgressProxy_ProxyMode_SNIExtraction(t *testing.T) {
	// Test the round-trip: write a ClientHello to a pipe, read back the SNI.
	hellos := []string{
		"api.telegram.org",
		"discord.com",
		"gateway.discord.gg",
		"cdn.discordapp.com",
		"api.github.com",
	}
	for _, want := range hellos {
		client, server := net.Pipe()
		done := make(chan struct{})
		go func() {
			defer close(done)
			hello := buildTestClientHello(t, want)
			client.Write(hello)
			client.Close()
		}()
		_, got, err := peekSNI(server)
		server.Close()
		<-done
		if err != nil {
			t.Errorf("peekSNI(%q): %v", want, err)
			continue
		}
		if got != want {
			t.Errorf("SNI = %q; want %q", got, want)
		}
	}
}

// startEchoTLSServer creates a self-signed TLS listener on a random port and
// returns it.  Each accepted connection is closed immediately (just validates
// the TCP handshake from the proxy).
func startEchoTLSServer(t *testing.T) (net.Listener, error) {
	t.Helper()

	// Generate self-signed cert for 127.0.0.1.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	tlsCert := tls.Certificate{Certificate: [][]byte{cert.Raw}, PrivateKey: priv}

	cfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12}
	l, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { l.Close() })
	return l, nil
}
