package collab

import (
	"fmt"
	"log"
	"os"
	"strings"

	"AegisClaw/internal/bootargs"
)

// TraceEnabled reports whether AEGIS_COLLAB_TRACE=1 (channel post / fan-out / STOMP tracing).
func TraceEnabled() bool {
	if strings.TrimSpace(os.Getenv("AEGIS_COLLAB_TRACE")) == "1" {
		return true
	}
	return bootargs.CollabTraceEnabled()
}

// Trace logs a single hop in the channel collaboration pipeline when tracing is enabled.
// stage examples: channel.post, channel.updated, fanout.start, fanout.deliver, stomp.notify
func Trace(component, stage, detail string) {
	if !TraceEnabled() {
		return
	}
	log.Printf("[collab-trace][%s][%s] %s", component, stage, detail)
}

// Tracef is Trace with printf-style formatting.
func Tracef(component, stage, format string, args ...interface{}) {
	Trace(component, stage, fmt.Sprintf(format, args...))
}
