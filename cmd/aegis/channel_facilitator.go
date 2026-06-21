package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"time"

	"AegisClaw/internal/channelfacilitator"
	"AegisClaw/internal/config"
	"AegisClaw/internal/transport/hubclient"

	"github.com/sirupsen/logrus"
)

// startChannelFacilitatorReceiver registers channel-facilitator on the hub and
// schedules turn-based delivery (logic lives in internal/channelfacilitator).
func startChannelFacilitatorReceiver() {
	hubPath := config.ResolveHubSocket()
	for {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		client, err := hubclient.DialUnix(hubPath, priv)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		requesterID := channelfacilitator.ComponentID
		regResp, err := client.Register(context.Background(), requesterID, pub, "phase1")
		if err != nil {
			client.Close()
			time.Sleep(1 * time.Second)
			continue
		}
		logrus.Infof("%s registered (assigned=%s) for turn-based channel propagation", requesterID, regResp.AssignedID)
		receiver := channelfacilitator.NewReceiver()

		for {
			msg, err := client.Receive(context.Background())
			if err != nil {
				break
			}
			if msg.Command == channelfacilitator.CmdUpdated {
				if payload, ok := msg.Payload.(map[string]interface{}); ok {
					chID, _ := payload["channel_id"].(string)
					from, _ := payload["from"].(string)
					channelfacilitator.TraceInbound(from, chID)
					go receiver.ProcessMessage(context.Background(), msg)
				}
				continue
			}
		}
		client.Close()
		time.Sleep(1 * time.Second)
	}
}