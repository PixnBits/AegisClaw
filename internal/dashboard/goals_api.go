package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"syscall"

	"AegisClaw/internal/channelid"
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
	if err := channelid.ValidateChannelID(channelID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), spaAPITimeout)
	defer cancel()

	planID := "plan_" + channelID
	stages := contracts.DefaultStages()

	raw, err := s.fetchRaw(ctx, "goal.submit", map[string]interface{}{
		"goal":       req.Goal,
		"channel_id": channelID,
	})
	if err != nil {
		msg := sanitize.Text(sanitize.ContextChat, err.Error())
		if msg == "" {
			msg = "goal submit failed"
		}
		http.Error(w, msg, goalSubmitStatus(err))
		return
	}
	if m, ok := raw.(map[string]interface{}); ok {
		if id, ok := m["plan_id"].(string); ok && id != "" {
			planID = id
		}
		if ch, ok := m["channel_id"].(string); ok && ch != "" {
			channelID = ch
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
	s.harnessMu.Lock()
	if s.harnessCache == nil {
		s.harnessCache = make(map[string]contracts.HarnessState)
	}
	s.harnessCache[channelID] = contracts.HarnessState{
		Plan: &contracts.Plan{
			PlanID:    planID,
			ChannelID: channelID,
			Goal:      req.Goal,
			Status:    contracts.PlanStatusActive,
			Stages:    stages,
		},
		Tasks: []contracts.NarrowTask{},
	}
	s.harnessMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
		"plan_id":    planID,
		"channel_id": channelID,
		"goal":       req.Goal,
		"stages":     stages,
		"preview":    true,
	})
}

// goalSubmitStatus maps a fetchRaw error onto an HTTP status. Input
// validation happens before fetchRaw and stays 400.
//
// 503 Service Unavailable: the daemon couldn't be reached or didn't answer
// in time. That covers UnavailableError (nil API client, noop bridge, a
// failed dial or Call), any net.Error including timeouts, connection
// refused, a missing socket (os.ErrNotExist), context.DeadlineExceeded and
// context.Canceled.
// 502 Bad Gateway: the daemon or hub answered with an error
// (UpstreamError), including an empty response. Anything else is also 502;
// it isn't the client's fault.
//
// The web-portal bridge returns transport failures from APIClient.Call and
// daemon Command=="error" replies as APIResponse.Success == false. fetchRaw
// types the two cases so errors.Is and errors.As can tell them apart.
func goalSubmitStatus(err error) int {
	if goalUnavailable(err) {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadGateway
}

func goalUnavailable(err error) bool {
	var unavail *UnavailableError
	if errors.As(err, &unavail) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, os.ErrNotExist) ||
		errors.Is(err, context.DeadlineExceeded) || // also a net.Error; kept for clarity
		errors.Is(err, context.Canceled)
}
