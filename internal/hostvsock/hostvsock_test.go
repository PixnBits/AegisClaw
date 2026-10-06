package hostvsock

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mdlayher/vsock"
)

func TestIsGuestCID(t *testing.T) {
	for _, cid := range []uint32{0, 1, 2, ^uint32(0)} {
		if IsGuestCID(cid) {
			t.Errorf("IsGuestCID(%d) = true", cid)
		}
	}
	for _, cid := range []uint32{3, 42, ^uint32(0) - 1} {
		if !IsGuestCID(cid) {
			t.Errorf("IsGuestCID(%d) = false", cid)
		}
	}
}

func TestAdmit(t *testing.T) {
	all := func(uint32) bool { return true }
	only42 := func(cid uint32) bool { return cid == 42 }
	cases := []struct {
		addr  net.Addr
		allow func(uint32) bool
		want  bool
	}{
		{&vsock.Addr{ContextID: 42}, only42, true},
		{&vsock.Addr{ContextID: 43}, only42, false},
		{&vsock.Addr{ContextID: 1}, all, false},
		{&vsock.Addr{ContextID: 2}, all, false},
		{&vsock.Addr{ContextID: 0}, all, false},
		{&vsock.Addr{ContextID: ^uint32(0)}, all, false},
		{&vsock.Addr{ContextID: 42}, nil, false},
		{&net.UnixAddr{Name: "x", Net: "unix"}, all, false},
		{nil, all, false},
	}
	for i, tc := range cases {
		if got := Admit(tc.addr, tc.allow); got != tc.want {
			t.Errorf("case %d (%v): Admit = %v, want %v", i, tc.addr, got, tc.want)
		}
	}
}

type fakeConn struct {
	net.Conn
	remote net.Addr
	closed bool
}

func (c *fakeConn) RemoteAddr() net.Addr { return c.remote }
func (c *fakeConn) Close() error         { c.closed = true; return nil }

type fakeListener struct {
	net.Listener
	conns []*fakeConn
}

func (l *fakeListener) Accept() (net.Conn, error) {
	if len(l.conns) == 0 {
		return nil, errors.New("done")
	}
	c := l.conns[0]
	l.conns = l.conns[1:]
	return c, nil
}

func TestGateClosesRefusedPeers(t *testing.T) {
	local := &fakeConn{remote: &vsock.Addr{ContextID: 1}}
	host := &fakeConn{remote: &vsock.Addr{ContextID: 2}}
	other := &fakeConn{remote: &vsock.Addr{ContextID: 43}}
	guest := &fakeConn{remote: &vsock.Addr{ContextID: 42}}
	l := Gate(&fakeListener{conns: []*fakeConn{local, host, other, guest}}, func(cid uint32) bool { return cid == 42 || cid == 1 || cid == 2 })
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if c != guest || guest.closed {
		t.Fatalf("Accept returned %v (guest closed=%v), want the CID 42 conn", c.RemoteAddr(), guest.closed)
	}
	if !local.closed || !host.closed {
		t.Fatalf("non-guest CIDs not closed: local=%v host=%v", local.closed, host.closed)
	}
	if !other.closed {
		t.Fatal("guest CID refused by allow was not closed")
	}
	if _, err := l.Accept(); err == nil {
		t.Fatal("Accept after the last conn should return the listener error")
	}
}

// hostBinaryDirs are the packages that run on the host.
var hostBinaryDirs = []string{"cmd/aegis", "cmd/aegishub"}

// forbiddenVsock reports an ungated AF_VSOCK listener in a host binary:
// a direct vsock listener constructor, a raw AF_VSOCK socket, or the guest
// helpers that accept vsock connections. Host code must use hostvsock.
func forbiddenVsock(file *ast.File, imports map[string]string) []string {
	var bad []string
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		pkg := imports[id.Name]
		switch {
		case pkg == "github.com/mdlayher/vsock" && strings.HasPrefix(sel.Sel.Name, "Listen"):
			bad = append(bad, id.Name+"."+sel.Sel.Name)
		case pkg == "github.com/mdlayher/vsock" && sel.Sel.Name == "FileListener":
			bad = append(bad, id.Name+"."+sel.Sel.Name)
		case sel.Sel.Name == "AF_VSOCK":
			bad = append(bad, id.Name+"."+sel.Sel.Name)
		case pkg == "AegisClaw/internal/transport/hubclient" && strings.HasPrefix(sel.Sel.Name, "AcceptVsock"):
			bad = append(bad, id.Name+"."+sel.Sel.Name)
		}
		return true
	})
	return bad
}

func importNames(file *ast.File) map[string]string {
	m := map[string]string{}
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		m[name] = path
	}
	return m
}

func scanDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, b := range forbiddenVsock(f, importNames(f)) {
			bad = append(bad, path+": "+b)
		}
	}
	return bad
}

func TestHostBinariesUseGatedVsock(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, d := range hostBinaryDirs {
		for _, b := range scanDir(t, filepath.Join(root, d)) {
			t.Errorf("host binary opens an ungated vsock listener (use internal/hostvsock): %s", b)
		}
	}
}

func TestForbiddenVsockDetector(t *testing.T) {
	src := `package main

import (
	"github.com/mdlayher/vsock"
	unix "golang.org/x/sys/unix"
	hc "AegisClaw/internal/transport/hubclient"
	"AegisClaw/internal/hostvsock"
)

func a() { vsock.Listen(1, nil) }
func b() { vsock.ListenContextID(3, 1, nil) }
func c() { _, _ = unix.Socket(unix.AF_VSOCK, 0, 0) }
func d() { hc.AcceptVsockHubBridgeConn(1) }
func e() { vsock.Dial(2, 1, nil) }
func f() { hostvsock.Listen(1, nil) }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := forbiddenVsock(f, importNames(f))
	want := []string{"vsock.Listen", "vsock.ListenContextID", "unix.AF_VSOCK", "hc.AcceptVsockHubBridgeConn"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("detector = %v, want %v", got, want)
	}
}
