package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

var webPortalInternalTarget string

func setWebPortalInternalTarget(target string) {
	webPortalInternalTarget = strings.TrimSpace(target)
}

// notifyWebPortalChannelActivity asks the web-portal microVM to publish STOMP
// channel.activity so browsers receive agent/PM posts without a full reload.
func notifyWebPortalChannelActivity(chID, from, content string) {
	target := webPortalInternalTarget
	if target == "" || chID == "" {
		return
	}
	body, err := json.Marshal(map[string]string{
		"channel_id": chID,
		"from":       from,
		"content":    content,
	})
	if err != nil {
		return
	}

	client, postURL := webPortalInternalHTTPClient(target)
	if client == nil || postURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, postURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		logrus.Debugf("web-portal channel-activity STOMP notify failed: %v", err)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		logrus.Debugf("web-portal channel-activity STOMP notify: HTTP %d", resp.StatusCode)
	}
}

func webPortalInternalHTTPClient(target string) (*http.Client, string) {
	if strings.HasPrefix(target, "fcvsock:") {
		udsPath, port, err := parseFcVsockTarget(target)
		if err != nil {
			return nil, ""
		}
		tr := &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialFirecrackerVsock(ctx, udsPath, port)
			},
		}
		return &http.Client{Transport: tr, Timeout: 3 * time.Second},
			"http://web-portal.internal/internal/realtime/channel-activity"
	}

	u := target
	if !strings.HasPrefix(u, "http") {
		u = "http://" + u
	}
	return &http.Client{Timeout: 3 * time.Second}, u + "/internal/realtime/channel-activity"
}
