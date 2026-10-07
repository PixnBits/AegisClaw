package main

import (
	"encoding/json"
	"io"
	"log"
	"time"
	"unicode/utf8"
)

const (
	hubSecurityEventPrefix           = "SECURITY "
	hubSecurityFieldCap              = 256
	hubSecretsUpdateRefusedEventName = "hub.secrets_update_refused"
)

var hubSecurityLogWriter = func() io.Writer { return log.Writer() }

type hubSecretsUpdateRefusedEvent struct {
	Event       string `json:"event"`
	Severity    string `json:"severity"`
	Time        string `json:"time"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Command     string `json:"command"`
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func logHubSecretsUpdateRefused(out io.Writer, msg Message, now time.Time) {
	ev := hubSecretsUpdateRefusedEvent{
		Event:       hubSecretsUpdateRefusedEventName,
		Severity:    "security",
		Time:        now.UTC().Format(time.RFC3339Nano),
		Source:      truncateUTF8(msg.Source, hubSecurityFieldCap),
		Destination: truncateUTF8(msg.Destination, hubSecurityFieldCap),
		Command:     truncateUTF8(msg.Command, hubSecurityFieldCap),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		line = []byte(`{"event":"` + hubSecretsUpdateRefusedEventName + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, hubSecurityEventPrefix+string(line)+"\n")
}
