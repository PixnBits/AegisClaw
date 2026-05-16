package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/PixnBits/AegisClaw/internal/api"
	"github.com/PixnBits/AegisClaw/internal/dashboard"
	"github.com/PixnBits/AegisClaw/internal/sandbox"
	"go.uber.org/zap"
)

// ... (imports and constants unchanged)

// Narrow the trusted actions for portal bridge to ONLY safe read-only operations.
// Privileged actions must go through normal peer-UID authorization.
func isTrustedPortalBridgeAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "chat.message",
		"chat.summarize",
		"worker.list",
		"worker.status",
		"skill.list",
		"skill.status",
		"court.decisions.list",
		"court.decisions.show",
		"sessions.list",
		"sessions.history",
		"sessions.status",
		"pr.list",
		"pr.get",
		"git.branches",
		"git.browse",
		"git.commits",
		"git.diff",
		"workspace.list":
		return true
	default:
		return false
	}
}

// rest of dashboard_daemon.go unchanged
