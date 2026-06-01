package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"AegisClaw/internal/chatstore"
	"AegisClaw/internal/config"
)

// Host-edge handlers for /api/chat/* (web-portal-vm.md).
// The Web Portal microVM often cannot reach AegisHub over vsock; the Host Daemon
// has a unix Hub socket and forwards session CRUD to the Store VM (store-vm.md).

func handleHostChatSessionsAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/chat/")
	switch {
	case path == "sessions" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		handleHostChatSessionsListOrCreate(w, r)
	case path == "history" && r.Method == http.MethodGet:
		handleHostChatHistory(w, r)
	case strings.HasPrefix(path, "sessions/") && r.Method == http.MethodPut:
		handleHostChatSessionSave(w, r, strings.TrimPrefix(path, "sessions/"))
	default:
		writeHostChatAPIError(w, http.StatusNotFound, "not found")
	}
}

func handleHostChatSessionsListOrCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		data, err := callStoreSessionsAction("sessions.list", nil)
		if err != nil {
			writeHostChatAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sessions, ok := data.([]interface{})
		if !ok || sessions == nil {
			sessions = []interface{}{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"sessions": sessions}) //nolint:errcheck
	case http.MethodPost:
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeHostChatAPIError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		body, err := callStoreSessionsAction("sessions.create", map[string]string{"title": req.Title})
		if err != nil {
			writeHostChatAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeHostChatSessionJSON(w, body)
	default:
		writeHostChatAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func handleHostChatHistory(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("session_id")
	if id == "" {
		writeHostChatAPIError(w, http.StatusBadRequest, "session_id required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	body, err := callStoreSessionsAction("sessions.history", map[string]string{"session_id": id})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeHostChatAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeHostChatAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHostChatSessionJSON(w, body)
}

func handleHostChatSessionSave(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" || strings.Contains(id, "/") {
		writeHostChatAPIError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeHostChatAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req["id"] = id
	w.Header().Set("Content-Type", "application/json")
	body, err := callStoreSessionsAction("sessions.save", req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeHostChatAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeHostChatAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeHostChatSessionJSON(w, body)
}

func callStoreSessionsAction(command string, payload interface{}) (interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := sendToComponentViaHubContext(ctx, "store", command, payload)
	if err != nil || resp == nil || !hostStoreSessionResponseOK(command, resp) {
		if err == nil {
			err = fmt.Errorf("empty or invalid store response for %s", command)
		}
		return callStoreSessionsLocalFallback(command, payload, err)
	}
	return resp, nil
}

func hostStoreSessionResponseOK(command string, resp interface{}) bool {
	switch command {
	case "sessions.list":
		_, ok := resp.([]interface{})
		return ok
	case "sessions.create", "sessions.history", "sessions.get", "sessions.save":
		m, ok := resp.(map[string]interface{})
		return ok && m["session"] != nil
	default:
		return resp != nil
	}
}

func writeHostChatSessionJSON(w http.ResponseWriter, body interface{}) {
	if body == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"session": nil}) //nolint:errcheck
		return
	}
	if m, ok := body.(map[string]interface{}); ok {
		if _, has := m["session"]; has {
			json.NewEncoder(w).Encode(m) //nolint:errcheck
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"session": m}) //nolint:errcheck
		return
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"session": body}) //nolint:errcheck
}

func callStoreSessionsLocalFallback(command string, payload interface{}, hubErr error) (interface{}, error) {
	store := chatstore.New(config.ResolveAegisDataDir() + "/chat-sessions.json")
	switch command {
	case "sessions.list":
		list, err := store.ListSummaries()
		if err != nil {
			return nil, err
		}
		out := make([]interface{}, 0, len(list))
		for _, s := range list {
			out = append(out, map[string]interface{}{
				"id": s.ID, "title": s.Title, "created_at": s.CreatedAt, "updated_at": s.UpdatedAt,
			})
		}
		return out, nil
	case "sessions.create":
		title := ""
		if m, ok := payload.(map[string]string); ok {
			title = m["title"]
		}
		sess, err := store.Create(title)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"session": sess}, nil
	case "sessions.history", "sessions.get":
		id := ""
		if m, ok := payload.(map[string]string); ok {
			id = m["session_id"]
		}
		sess, ok, err := store.Get(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errHubError("session not found")
		}
		return map[string]interface{}{"session": sess}, nil
	case "sessions.save":
		m, ok := payload.(map[string]interface{})
		if !ok {
			return nil, errHubError("invalid payload")
		}
		id, _ := m["id"].(string)
		sess := chatstore.Session{ID: id}
		if title, ok := m["title"].(string); ok {
			sess.Title = title
		}
		if rawMsgs, ok := m["messages"].([]interface{}); ok {
			sess.Messages = decodeHostChatMessages(rawMsgs)
		}
		if err := store.Save(sess); err != nil {
			return nil, err
		}
		updated, ok, err := store.Get(id)
		if err != nil || !ok {
			return nil, errHubError("failed to reload session")
		}
		return map[string]interface{}{"session": updated}, nil
	default:
		return nil, hubErr
	}
}

func decodeHostChatMessages(raw []interface{}) []chatstore.Message {
	out := make([]chatstore.Message, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		msg := chatstore.Message{}
		if role, ok := m["role"].(string); ok {
			msg.Role = role
		}
		if content, ok := m["content"].(string); ok {
			msg.Content = content
		}
		if model, ok := m["model"].(string); ok {
			msg.Model = model
		}
		if tc, ok := m["tool_calls"]; ok {
			msg.ToolCalls, _ = json.Marshal(tc)
		}
		if tt, ok := m["thinking_trace"]; ok {
			msg.ThinkingTrace, _ = json.Marshal(tt)
		}
		out = append(out, msg)
	}
	return out
}

type hubError string

func (e hubError) Error() string { return string(e) }

func errHubError(msg string) error { return hubError(msg) }

func writeHostChatAPIError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
