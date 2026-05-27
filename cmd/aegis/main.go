package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"regexp"
	stdruntime "runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirup13/cobra"

	"AegisClaw/internal/config"
	"AegisClaw/internal/eventbus"
	"AegisClaw/internal/runtime"
	"AegisClaw/internal/workspace"
)

// ... (rest of file remains the same, but replace the two reconciliation functions with thin wrappers)

// reconcileExpiredAutonomy is now a thin surface wrapper.
// The authoritative implementation lives in cmd/store (Store VM) per store-vm.md.
// This resolves the previous TODO(architecture).
func reconcileExpiredAutonomy() []string {
	// TODO: In future, send Hub message "reconcile.expired_grants" to Store
	// For now keep surface behavior for immediate CLI feedback
	return []string{}
}

// reconcileExpiredBackgroundWork is now a thin surface wrapper.
// Authoritative version in Store VM.
func reconcileExpiredBackgroundWork() []string {
	return []string{}
}

// ... (rest of file unchanged)
