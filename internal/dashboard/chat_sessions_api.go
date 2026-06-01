package dashboard

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Chat session REST handlers (thin layer). Persistence lives in the Store VM;
// the Web Portal forwards CRUD via bridge actions sessions.* per web-portal.md.

func (s *Server) handleChatSessionsListOrCreate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		data, err := s.fetchRaw(r.Context(), "sessions.list", nil)
		if err != nil {
			writeChatAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sessions, _ := data.([]interface{})
		if sessions == nil {
			sessions = []interface{}{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"sessions": sessions}) //nolint:errcheck
	case http.MethodPost:
		var req struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeChatAPIError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		data, err := s.fetchRaw(r.Context(), "sessions.create", map[string]string{"title": req.Title})
		if err != nil {
			writeChatAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		json.NewEncoder(w).Encode(data) //nolint:errcheck
	default:
		writeChatAPIError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeChatAPIError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}
	id := r.URL.Query().Get("session_id")
	if id == "" {
		writeChatAPIError(w, http.StatusBadRequest, "session_id required")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	data, err := s.fetchRaw(r.Context(), "sessions.history", map[string]string{"session_id": id})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeChatAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeChatAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

func (s *Server) handleChatSessionSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeChatAPIError(w, http.StatusMethodNotAllowed, "PUT required")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/chat/sessions/")
	if id == "" || strings.Contains(id, "/") {
		writeChatAPIError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeChatAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req["id"] = id
	w.Header().Set("Content-Type", "application/json")
	data, err := s.fetchRaw(r.Context(), "sessions.save", req)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeChatAPIError(w, http.StatusNotFound, err.Error())
			return
		}
		writeChatAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	json.NewEncoder(w).Encode(data) //nolint:errcheck
}

func writeChatAPIError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
