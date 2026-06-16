package portalstomp

import (
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// ServeWebSocket upgrades HTTP to WebSocket and runs a minimal STOMP 1.2 subset.
func ServeWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	server := websocket.Server{
		Handler: func(ws *websocket.Conn) {
			defer ws.Close()
			sess := NewSession(hub)
			defer sess.Close()

			done := make(chan struct{})
			go func() {
				defer close(done)
				for frame := range sess.Outbound() {
					if _, err := io.WriteString(ws, frame); err != nil {
						return
					}
				}
			}()

			buf := make([]byte, 64*1024)
			for {
				_ = ws.SetReadDeadline(time.Now().Add(120 * time.Second))
				n, err := ws.Read(buf)
				if err != nil {
					return
				}
				raw := string(buf[:n])
				for _, chunk := range strings.Split(raw, "\x00") {
					chunk = strings.TrimSpace(chunk)
					if chunk == "" {
						continue
					}
					chunk += "\x00"
					cmd, headers, body, ok := ParseFrame(chunk)
					if !ok {
						continue
					}
					sess.HandleFrame(cmd, headers, body)
				}
				select {
				case <-done:
					return
				default:
				}
			}
		},
	}
	server.ServeHTTP(w, r)
}
