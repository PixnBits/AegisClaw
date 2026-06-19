package collab

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func TestTraceEnabled(t *testing.T) {
	t.Setenv("AEGIS_COLLAB_TRACE", "")
	if TraceEnabled() {
		t.Fatal("expected disabled")
	}
	t.Setenv("AEGIS_COLLAB_TRACE", "1")
	if !TraceEnabled() {
		t.Fatal("expected enabled")
	}
}

func TestTraceLogsWhenEnabled(t *testing.T) {
	t.Setenv("AEGIS_COLLAB_TRACE", "1")
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	Trace("store", "channel.post", "ch=main from=user")
	if !strings.Contains(buf.String(), "[collab-trace][store][channel.post]") {
		t.Fatalf("missing trace line: %q", buf.String())
	}
}
