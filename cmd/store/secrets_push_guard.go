package main

import (
	"encoding/json"
	"io"
	"time"

	"AegisClaw/internal/hubids"
)

// storeSecretsPushRefusedEvent is the SECURITY event for a secrets.push from
// a source that may not send it.
const storeSecretsPushRefusedEvent = "store.secrets_push_refused"

// isSecretsPushSource reports whether source may send secrets.push: the host
// daemon and its daemon-internal / daemon-internal-N clients, matched as exact
// hubids families. Guests, network-boundary, the portal and look-alikes such
// as daemon-internalx are refused.
func isSecretsPushSource(source string) bool {
	return source == "daemon" || hubids.InFamily(source, "daemon-internal")
}

type storeSourceRefusedEvent struct {
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Time        string `json:"time"`
	Command     string `json:"command"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// refuseSecretsPush sets an ERR_PERMISSION_DENIED reply and writes one
// SECURITY line. Source and destination are capped and JSON-encoded, so they
// can't break the line.
func refuseSecretsPush(out io.Writer, msg Message, response *Message, now time.Time) {
	response.Command = "error"
	response.Payload = "ERR_PERMISSION_DENIED: secrets.push is host-only"
	line, err := json.Marshal(storeSourceRefusedEvent{
		Event:       storeSecretsPushRefusedEvent,
		Severity:    "security",
		Time:        now.UTC().Format(time.RFC3339Nano),
		Command:     truncateUTF8(msg.Command, securityFieldCap),
		Source:      truncateUTF8(msg.Source, securityFieldCap),
		Destination: truncateUTF8(msg.Destination, securityFieldCap),
	})
	if err != nil {
		line = []byte(`{"event":"` + storeSecretsPushRefusedEvent + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, securityEventPrefix+string(line)+"\n")
}
