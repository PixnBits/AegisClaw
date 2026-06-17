package dashboard

import (
	"context"
	"encoding/json"
	"net/http"

	"AegisClaw/internal/dashboard/contracts"
	"AegisClaw/internal/dashboard/sanitize"
)

func (s *Server) handleAPIGoals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Goal      string `json:"goal"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Goal == "" {
		http.Error(w, "goal required", http.StatusBadRequest)
		return
	}
	req.Goal = sanitize.Text(sanitize.ContextChat, req.Goal)
	channelID := req.ChannelID
	if channelID == "" {
		channelID = "main"
	}

	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	planID := "plan_" + channelID
	stages := contracts.DefaultStages()

	if raw, err := s.fetchRaw(ctx, "goal.submit", map[string]interface{}{
		"goal":       req.Goal,
		"channel_id": channelID,
	}); err == nil {
		if m, ok := raw.(map[string]interface{}); ok {
			if id, ok := m["plan_id"].(string); ok && id != "" {
				planID = id
			}
			if ch, ok := m["channel_id"].(string); ok && ch != "" {
				channelID = ch
			}
		}
	}

	event := contracts.HarnessPlanCreated{
		Type:      contracts.TypeHarnessPlanCreated,
		PlanID:    planID,
		ChannelID: channelID,
		Goal:      req.Goal,
		Stages:    stages,
	}
	s.stompPublisher().PublishHarness(planID, channelID, event)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"plan_id":    planID,
		"channel_id": channelID,
		"goal":       req.Goal,
		"stages":     stages,
		"preview":    true,
	})
}