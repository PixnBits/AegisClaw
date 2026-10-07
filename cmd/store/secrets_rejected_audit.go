package main

import (
	"encoding/json"
	"io"
	"sync/atomic"
	"time"
)

const storeSecretsUpdateRejectedEventName = "store.secrets_update_rejected"

var secretsUpdateRejected atomic.Uint64

type secretsUpdateRejectedEvent struct {
	Event    string `json:"event"`
	Severity string `json:"severity"`
	Time     string `json:"time"`
	Source   string `json:"source"`
	Command  string `json:"command"`
	Error    string `json:"error"`
}

func logSecretsUpdateRejected(out io.Writer, msg Message, errText string, now time.Time) {
	ev := secretsUpdateRejectedEvent{
		Event:    storeSecretsUpdateRejectedEventName,
		Severity: "security",
		Time:     now.UTC().Format(time.RFC3339Nano),
		Source:   truncateUTF8(msg.Source, securityFieldCap),
		Command:  truncateUTF8(msg.Command, securityFieldCap),
		Error:    truncateUTF8(errText, securityFieldCap),
	}
	line, err := json.Marshal(ev)
	if err != nil {
		line = []byte(`{"event":"` + storeSecretsUpdateRejectedEventName + `","severity":"security","marshal_error":true}`)
	}
	_, _ = io.WriteString(out, securityEventPrefix+string(line)+"\n")
}
