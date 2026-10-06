package main

import (
	"fmt"

	"AegisClaw/internal/channelid"
)

// pmGoalHubReply is one hub send. Command "error" is an error reply the
// sender did not already turn into a Go error. sendToComponentViaHub does
// that conversion; tests and a raw reply still set Command.
type pmGoalHubReply struct {
	Command string
	Payload interface{}
}

// pmGoalHubSend is the hub send injected into ensurePMGoalChannel.
type pmGoalHubSend func(target, cmd string, payload interface{}) (pmGoalHubReply, error)

func sendPMGoalViaHub(target, cmd string, payload interface{}) (pmGoalHubReply, error) {
	data, err := sendToComponentViaHub(target, cmd, payload)
	if err != nil {
		return pmGoalHubReply{}, err
	}
	return pmGoalHubReply{Payload: data}, nil
}

// ensurePMGoalChannel checks the id, then creates the channel only when
// channel.get fails or returns no payload. An invalid id does not touch the
// hub. A create error or an error reply stops the caller.
func ensurePMGoalChannel(chID string, send pmGoalHubSend) error {
	if err := channelid.ValidateChannelID(chID); err != nil {
		return err
	}
	if send == nil {
		return fmt.Errorf("hub send not configured")
	}
	got, err := send("store", "channel.get", map[string]string{"id": chID})
	if err == nil && got.Command != "error" && got.Payload != nil {
		return nil
	}
	created, err := send("store", "channel.create", map[string]interface{}{"id": chID})
	if err != nil {
		return fmt.Errorf("channel.create: %w", err)
	}
	if created.Command == "error" {
		msg := fmt.Sprint(created.Payload)
		if msg == "" || msg == "<nil>" {
			msg = "channel.create refused"
		}
		return fmt.Errorf("channel.create: %s", msg)
	}
	return nil
}
