package hubgit

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"

	"AegisClaw/internal/storegit"
)

// Serve reads one `git-connect <service> <url>` line from c, compares
// sessionTenant to the URL tenant, and on match splices git pack to
// storeGitSock using the session tenant (not the URL, not helper env).
// Missing session or mismatch writes "deny tenancy acl: not your tenant"
// and returns without dialing Store.
func Serve(c net.Conn, sessionTenant, storeGitSock string) {
	defer c.Close()
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		return
	}
	line = strings.TrimSpace(line)
	const prefix = "git-connect "
	if !strings.HasPrefix(line, prefix) {
		_, _ = fmt.Fprintf(c, "deny unknown git-connect\n")
		return
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	service, url, ok := strings.Cut(rest, " ")
	if !ok {
		_, _ = fmt.Fprintf(c, "deny bad git-connect\n")
		return
	}
	target, repo, parsed := storegit.ParseURL(url)
	if strings.TrimSpace(sessionTenant) == "" || !parsed || sessionTenant != target {
		_, _ = fmt.Fprintf(c, "deny tenancy acl: not your tenant\n")
		return
	}
	if strings.TrimSpace(storeGitSock) == "" {
		_, _ = fmt.Fprintf(c, "deny store git socket\n")
		return
	}
	storec, err := net.Dial("unix", storeGitSock)
	if err != nil {
		_, _ = fmt.Fprintf(c, "deny store git socket\n")
		return
	}
	defer storec.Close()
	if _, err := fmt.Fprintf(storec, "%s %s %s\n", service, sessionTenant, repo); err != nil {
		return
	}
	if _, err := fmt.Fprintf(c, "ok\n"); err != nil {
		return
	}
	errc := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(storec, br)
		errc <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(c, storec)
		errc <- struct{}{}
	}()
	<-errc
	<-errc
}
