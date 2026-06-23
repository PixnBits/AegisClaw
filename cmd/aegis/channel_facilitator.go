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

const channelFacilitatorMaxWorkers = 16

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

		// Bounded worker pool to avoid unbounded goroutines on high update volume (Copilot feedback).
		// Per-channel actors inside facilitator still serialize processing per ch.
		workerSem := make(chan struct{}, channelFacilitatorMaxWorkers)

		for {
			msg, err := client.Receive(context.Background())
			if err != nil {
				break
			}
			isUpdated := msg.Command == channelfacilitator.CmdUpdated
			isTurnResult := msg.Command == channelfacilitator.CmdTurnResult
			if isUpdated {
				if payload, ok := msg.Payload.(map[string]interface{}); ok {
					chID, _ := payload["channel_id"].(string)
					from, _ := payload["from"].(string)
					channelfacilitator.TraceInbound(from, chID)
				}
			}
			if isUpdated || isTurnResult {
				// Dispatch both updated (for scheduling) and turn_result (to clear pending/outcome for observability).
				// Use semaphore to bound concurrency (Copilot #3).
				select {
				case workerSem <- struct{}{}:
					go func(m hubclient.Message) {
						defer func() { <-workerSem }()
						receiver.ProcessMessage(context.Background(), m)
					}(msg)
				default:
					// Pool full; process synchronously to backpressure (rare).
					receiver.ProcessMessage(context.Background(), msg)
				}
			}
		}
		client.Close()
		time.Sleep(1 * time.Second)
	}
}