package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"log"
	"sync"
	"time"

	"AegisClaw/internal/dashboard"
	"AegisClaw/internal/transport/hubclient"

	"github.com/mdlayher/vsock"
)

// resilientBridgeClient keeps a live Hub or portal-bridge session for the web-portal
// guest. It accepts host re-dials on the inverted hub bridge (:9101), reconnects after
// stale connections, and honors caller context deadlines so API handlers cannot hang
// indefinitely behind a dead bridge.
type resilientBridgeClient struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey

	mu      sync.RWMutex
	session *bridgeSession
	noop    dashboard.APIClient
}

func newResilientBridgeClient() dashboard.APIClient {
	pub, priv := portalBridgeKey()
	r := &resilientBridgeClient{
		pub:  pub,
		priv: priv,
		noop: &noopAPIClient{},
	}
	if runningInFirecrackerGuest() {
		r.startInvertedHubBridgeListener()
	}
	go r.maintainConnection()
	return r
}

func (r *resilientBridgeClient) Call(ctx context.Context, action string, payload json.RawMessage) (*dashboard.APIResponse, error) {
	sess := r.currentSession()
	if sess == nil {
		return r.noop.Call(ctx, action, payload)
	}
	resp, err := sess.call(ctx, action, payload)
	if err != nil && bridgeIOError(err) {
		r.invalidateSession(sess)
	}
	return resp, err
}

func (r *resilientBridgeClient) currentSession() *bridgeSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.session
}

func (r *resilientBridgeClient) setSession(sess *bridgeSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session != nil {
		r.session.close()
	}
	r.session = sess
}

func (r *resilientBridgeClient) invalidateSession(stale *bridgeSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.session == stale {
		r.session.close()
		r.session = nil
	}
}

// startInvertedHubBridgeListener keeps vsock :9101 open so the host daemon can
// re-dial after guest_hub_bridge reconnects (one-shot accept was the root cause of
// permanent API hangs after the first bridge drop).
func (r *resilientBridgeClient) startInvertedHubBridgeListener() {
	go func() {
		port := uint32(hubclient.GuestHubBridgePort)
		for {
			ln, err := vsock.Listen(port, nil)
			if err != nil {
				log.Printf("web-portal: inverted hub bridge listen :%d failed: %v", port, err)
				time.Sleep(500 * time.Millisecond)
				continue
			}
			log.Printf("web-portal: inverted hub bridge listening on vsock :%d", port)
			for {
				conn, err := ln.Accept()
				if err != nil {
					break
				}
				sess, err := newBridgeSession(conn, r.priv, false)
				if err != nil {
					log.Printf("web-portal: inverted hub bridge session: %v", err)
					_ = conn.Close()
					continue
				}
				r.setSession(sess)
				log.Println("web-portal: inverted hub bridge connected (host dial)")
			}
			_ = ln.Close()
			time.Sleep(200 * time.Millisecond)
		}
	}()
}

func (r *resilientBridgeClient) maintainConnection() {
	delay := 500 * time.Millisecond
	attempt := 0
	for {
		if r.currentSession() != nil {
			time.Sleep(2 * time.Second)
			continue
		}
		attempt++
		sess, err := r.tryDialSession()
		if err == nil {
			r.setSession(sess)
			log.Println("web-portal: hub/portal bridge connected")
			delay = 500 * time.Millisecond
			attempt = 0
		} else {
			if attempt == 1 || attempt%10 == 0 {
				log.Printf("WARNING: web-portal hub bridge attempt %d: %v", attempt, err)
			}
			time.Sleep(delay)
			if delay < 5*time.Second {
				delay += 250 * time.Millisecond
			}
		}
	}
}

func (r *resilientBridgeClient) tryDialSession() (*bridgeSession, error) {
	conn, viaHost, err := dialOutboundHubOrPortalBridge()
	if err != nil {
		return nil, err
	}
	return newBridgeSession(conn, r.priv, viaHost)
}