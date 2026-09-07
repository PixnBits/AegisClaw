package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"AegisClaw/internal/storegit"
)

type storeGitServer struct {
	lns   []net.Listener
	paths []string
}

func (s *storeGitServer) Close() error {
	if s == nil {
		return nil
	}
	for _, ln := range s.lns {
		_ = ln.Close()
	}
	for _, p := range s.paths {
		_ = os.Remove(p)
	}
	return nil
}

var bareMu sync.Mutex

func startStoreGitServer() (*storeGitServer, error) {
	p := storegit.HubPrivateGitSocket()
	if p == "" {
		return nil, fmt.Errorf("AEGIS_STORE_GIT_SOCKET required (no git.sock)")
	}
	paths := []string{p}
	s := &storeGitServer{}
	var firstErr error
	for _, p := range paths {
		_ = os.Remove(p)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ln, err := net.Listen("unix", p)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := os.Chmod(p, 0o600); err != nil {
			_ = ln.Close()
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		s.lns = append(s.lns, ln)
		s.paths = append(s.paths, p)
		go acceptGit(ln)
	}
	if len(s.lns) == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("no git sockets listened")
		}
		return nil, firstErr
	}
	return s, nil
}

func acceptGit(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleGitConn(conn)
	}
}

func handleGitConn(conn net.Conn) {
	defer conn.Close()
	header, err := readHeaderLine(conn)
	if err != nil {
		return
	}
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return
	}
	service, tenant, repo := fields[0], fields[1], fields[2]
	proto := ""
	if len(fields) > 3 {
		proto = strings.Join(fields[3:], " ")
	}
	bare, err := ensureBareRepo(tenant, repo)
	if err != nil {
		return
	}
	abs, err := filepath.Abs(bare)
	if err != nil {
		return
	}
	cmd := gitServiceCommand(service, abs)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if proto != "" {
		cmd.Env = append(cmd.Env, "GIT_PROTOCOL="+proto)
	}

	f, err := connFile(conn)
	if err != nil {
		return
	}
	defer f.Close()
	cmd.Stdin = f
	cmd.Stdout = f
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func gitServiceCommand(service, bare string) *exec.Cmd {
	switch service {
	case "git-receive-pack", "receive-pack":
		return exec.Command("git", "receive-pack", bare)
	default:
		return exec.Command("git", "upload-pack", "--", bare)
	}
}

func connFile(conn net.Conn) (*os.File, error) {
	type filer interface {
		File() (*os.File, error)
	}
	f, ok := conn.(filer)
	if !ok {
		return nil, fmt.Errorf("git conn is not a file")
	}
	return f.File()
}

func readHeaderLine(r io.Reader) (string, error) {
	var b []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return string(b), nil
			}
			if buf[0] != '\r' {
				b = append(b, buf[0])
			}
			if len(b) > 4096 {
				return "", fmt.Errorf("header too long")
			}
		}
		if err != nil {
			if len(b) > 0 && err == io.EOF {
				return string(b), nil
			}
			return "", err
		}
	}
}

func ensureBareRepo(tenant, repo string) (string, error) {
	if !storegit.ValidName(tenant) || !storegit.ValidName(repo) {
		return "", fmt.Errorf("invalid tenant or repo")
	}
	bareMu.Lock()
	defer bareMu.Unlock()
	bare := storegit.BarePath(tenant, repo)
	if _, err := os.Stat(filepath.Join(bare, "HEAD")); err == nil {
		return bare, nil
	}
	if err := os.MkdirAll(filepath.Dir(bare), 0o700); err != nil {
		return "", err
	}
	cmd := exec.Command("git", "init", "--bare", bare)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init --bare: %s: %w", strings.TrimSpace(string(out)), err)
	}
	_ = exec.Command("git", "-C", bare, "symbolic-ref", "HEAD", "refs/heads/main").Run()
	_ = exec.Command("git", "-C", bare, "config", "receive.denyNonFastForwards", "true").Run()
	_ = exec.Command("git", "-C", bare, "config", "receive.denyDeletes", "true").Run()
	hook := filepath.Join(bare, "hooks", "pre-receive")
	body := "#!/bin/sh\n" +
		"zero=0000000000000000000000000000000000000000\n" +
		"while read old new ref; do\n" +
		"  if [ \"$new\" = \"$zero\" ]; then echo deny-delete-ref >&2; exit 1; fi\n" +
		"  if [ \"$old\" != \"$zero\" ]; then\n" +
		"    git merge-base --is-ancestor \"$old\" \"$new\" || { echo deny-non-fast-forward >&2; exit 1; }\n" +
		"  fi\n" +
		"  if [ \"$old\" = \"$zero\" ]; then\n" +
		"    names=$(git ls-tree -r --name-only \"$new\")\n" +
		"  else\n" +
		"    names=$(git diff-tree -r --name-only \"$old\" \"$new\")\n" +
		"  fi\n" +
		"  echo \"$names\" | grep -E '(^|/)\\.gitmodules$' >/dev/null && { echo deny-submodule >&2; exit 1; } || true\n" +
		"  echo \"$names\" | grep -E '(^|/)\\.lfsconfig$' >/dev/null && { echo deny-lfs >&2; exit 1; } || true\n" +
		"done\n" +
		"exit 0\n"
	if err := os.WriteFile(hook, []byte(body), 0o700); err != nil {
		return "", err
	}
	return bare, nil
}
