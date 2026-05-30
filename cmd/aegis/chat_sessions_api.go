package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"AegisClaw/internal/chatstore"
	"AegisClaw/internal/config"
)

var chatSessionsStore *chatstore.Store

func initChatSessionsStore() {
	dir := config.ResolveAegisDataDir()
	chatSessionsStore = chatstore.New(dir + "/chat-sessions.json")
}

// handleChatSessionsAPI serves host-persisted chat session CRUD at the reverse
// proxy edge (/api/chat/*). This replaces browser localStorage so conversations
// are shared across browsers/devices hitting the same daemon.
func handleChatSessionsAPI(w http.ResponseWriter, r *http.Request) {
	if chatSessionsStore == nil {
		initChatSessionsStore()
	}

	w.Header().Set("Content-Type", "application/json")

	path := strings.TrimPrefix(r.URL.Path, "/api/chat/")
	switch {
	case path == "sessions" && r.Method == http.MethodGet:
		list, err := chatSessionsStore.ListSummaries()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if list == nil {
			list = []chatstore.Summary{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"sessions": list}) //nolint:errcheck
		return

	case path == "sessions" && r.Method == http.MethodPost:
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		sess, err := chatSessionsStore.Create(req.Title)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"session": sess}) //nolint:errcheck
		return

	case path == "history" && r.Method == http.MethodGet:
		id := r.URL.Query().Get("session_id")
		if id == "" {
			writeJSONError(w, http.StatusBadRequest, "session_id required")
			return
		}
		sess, ok, err := chatSessionsStore.Get(id)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"session": sess}) //nolint:errcheck
		return

	case strings.HasPrefix(path, "sessions/") && r.Method == http.MethodPut:
		id := strings.TrimPrefix(path, "sessions/")
		if id == "" || strings.Contains(id, "/") {
			writeJSONError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		var req chatstore.Session
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		req.ID = id
		if err := chatSessionsStore.Save(req); err != nil {
			if strings.Contains(err.Error(), "not found") {
				writeJSONError(w, http.StatusNotFound, err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sess, ok, err := chatSessionsStore.Get(id)
		if err != nil || !ok {
			writeJSONError(w, http.StatusInternalServerError, "failed to reload session")
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"session": sess}) //nolint:errcheck
		return

	default:
		writeJSONError(w, http.StatusNotFound, "not found")
	}
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
