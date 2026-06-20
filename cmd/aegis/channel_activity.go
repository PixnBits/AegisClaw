package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"AegisClaw/internal/collab"
)

var channelActivityFanOutMu sync.Mutex

// fanOutChannelActivity delivers channel.activity to each channel member except the poster.
// Agents reply immediately (accepted/ignored) and decide locally whether to post back.
func fanOutChannelActivity(chID, from, content string) {
	if chID == "" {
		return
	}
	go func() {
		timeout := channelActivityTimeoutFromEnv()
		if err := fanOutChannelActivitySync(chID, from, content, timeout); err != nil {
			log.Printf("channel activity fan-out for %s: %v", chID, err)
		}
	}()
}

func fanOutChannelActivitySync(chID, from, content string, perMemberTimeout time.Duration) error {
	channelActivityFanOutMu.Lock()
	defer channelActivityFanOutMu.Unlock()

	collab.Tracef("daemon", "fanout.start", "ch=%s from=%s", chID, from)

	chData, err := sendToComponentViaEphemeralHubRetry("store", "channel.get", map[string]interface{}{"channel_id": chID}, 10*time.Second)
	if err != nil {
		return fmt.Errorf("channel.get: %w", err)
	}
	roles := prioritizeFanOutRoles(extractChannelMemberRoles(chData))
	if len(roles) == 0 {
		collab.Tracef("daemon", "fanout.skip", "ch=%s reason=no_members", chID)
		return nil
	}
	collab.Tracef("daemon", "fanout.members", "ch=%s count=%d roles=%s", chID, len(roles), strings.Join(roles, ","))

	// PM is on-demand per channel; boot before fan-out so project-manager-main is hub-ready.
	for _, role := range roles {
		if role == "project-manager" {
			_, _ = sendToComponentViaEphemeralHubRetry("daemon-orchestrator", "ensure.role", map[string]interface{}{
				"role":    "project-manager",
				"channel": chID,
			}, 30*time.Second)
			break
		}
	}

	var errs []string
	payload := map[string]interface{}{
		"channel_id": chID,
		"from":       from,
		"content":    content,
	}
	deliverToRoles := func(pass string, targetRoles []string) {
		var wg sync.WaitGroup
		var errMu sync.Mutex
		for _, role := range targetRoles {
			if role == "" || shouldSkipMemberFanOut(role, from) {
				continue
			}
			wg.Add(1)
			go func(role string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), perMemberTimeout)
				defer cancel()
				err := deliverChannelActivity(ctx, role, chID, payload)
				if err != nil {
					errMu.Lock()
					errs = append(errs, fmt.Sprintf("%s: %v", role, err))
					errMu.Unlock()
					log.Printf("channel activity delivery to %s failed (%s): %v", role, pass, err)
				}
			}(role)
		}
		wg.Wait()
	}
	deliverToRoles("pass1", roles)
	if len(errs) > 0 {
		retry := make([]string, 0, len(errs))
		for _, e := range errs {
			if idx := strings.Index(e, ":"); idx > 0 {
				retry = append(retry, strings.TrimSpace(e[:idx]))
			}
		}
		if len(retry) > 0 {
			errs = nil
			time.Sleep(2 * time.Second)
			deliverToRoles("pass2", retry)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delivery failures (%d): %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

func deliverChannelActivity(ctx context.Context, role, chID string, payload map[string]interface{}) error {
	var lastErr error
	for _, dest := range activityDestinations(role, chID) {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			_, err := sendToComponentViaEphemeralHubContext(ctx, dest, "channel.activity", payload)
			if err == nil {
				collab.Tracef("daemon", "fanout.deliver.ok", "dest=%s role=%s ch=%s", dest, role, chID)
				return nil
			}
			lastErr = err
			if isHubDestinationNotFound(err) {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return err
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no hub destination for role %q on channel %q", role, chID)
}

func activityDestinations(role, chID string) []string {
	if role == "project-manager" && chID != "" {
		return []string{"project-manager-" + chID, "project-manager"}
	}
	return []string{role}
}

func shouldSkipMemberFanOut(memberRole, from string) bool {
	if from == memberRole {
		return true
	}
	if memberRole == "project-manager" && strings.HasPrefix(from, "project-manager") {
		return true
	}
	if strings.HasPrefix(memberRole, "court-persona-") && from == memberRole {
		return true
	}
	return false
}

func prioritizeFanOutRoles(roles []string) []string {
	var pm, courts, rest []string
	for _, role := range roles {
		switch role {
		case "project-manager":
			pm = append(pm, role)
		default:
			if strings.HasPrefix(role, "court-persona-") {
				courts = append(courts, role)
			} else {
				rest = append(rest, role)
			}
		}
	}
	for i, role := range courts {
		if role == "court-persona-user-advocate" {
			courts[0], courts[i] = courts[i], courts[0]
			break
		}
	}
	return append(append(pm, courts...), rest...)
}

func channelActivityTimeoutFromEnv() time.Duration {
	if v := strings.TrimSpace(os.Getenv("AEGIS_CHANNEL_ACTIVITY_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	if v := strings.TrimSpace(os.Getenv("AEGIS_CHANNEL_NOTIFY_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 90 * time.Second
}

func extractChannelMemberRoles(chData interface{}) []string {
	m, ok := chData.(map[string]interface{})
	if !ok {
		return nil
	}
	membersRaw, ok := m["members"].([]interface{})
	if !ok {
		return nil
	}
	seen := make(map[string]struct{})
	var roles []string
	for _, item := range membersRaw {
		member, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := member["role"].(string)
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, dup := seen[role]; dup {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	return roles
}
