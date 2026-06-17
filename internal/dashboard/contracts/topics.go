package contracts

import (
	"fmt"
	"strings"
)

// STOMP topic naming per docs/specs/web-portal/real-time-contracts.md.
// Legacy topics remain allowed during migration (dual-publish on server).

const (
	TopicOverviewStats      = "/topic/overview.stats"
	TopicCanvasEvents       = "/topic/canvas.events"
	TopicApprovalsPending   = "/topic/approvals.pending"
	TopicConversationPrefix = "/topic/conversation."
	TopicChannelPrefix      = "/topic/channel."
	TopicHarnessPrefix      = "/topic/harness."
	TopicProposalPrefix     = "/topic/proposal."

	// LegacyChannelMessagesPrefix is deprecated; use TopicChannelPrefix.
	LegacyChannelMessagesPrefix = "/topic/channels."
)

// View identifies a portal screen for subscription scoping.
type View string

const (
	ViewHome        View = "home"
	ViewChannels    View = "channels"
	ViewDashboard   View = "dashboard"
	ViewCourt       View = "court"
	ViewCanvas      View = "canvas"
	ViewAgents      View = "agents"
	ViewTrace       View = "trace"
	ViewSettings    View = "settings"
)

// ChannelActivityTopic returns the canonical channel activity destination.
func ChannelActivityTopic(channelID string) string {
	return TopicChannelPrefix + channelID + ".activity"
}

// LegacyChannelMessagesTopic returns the pre-spec channel messages topic.
func LegacyChannelMessagesTopic(channelID string) string {
	return LegacyChannelMessagesPrefix + channelID + ".messages"
}

// HarnessUpdatesTopic returns harness delta updates for a plan.
func HarnessUpdatesTopic(planID string) string {
	return TopicHarnessPrefix + planID + ".updates"
}

// ConversationUpdatesTopic returns per-session conversation updates.
func ConversationUpdatesTopic(sessionID string) string {
	return TopicConversationPrefix + sessionID + ".updates"
}

// ProposalUpdatesTopic returns per-proposal vote/status updates.
func ProposalUpdatesTopic(proposalID string) string {
	return TopicProposalPrefix + proposalID + ".updates"
}

// IsAllowedTopic reports whether a browser may subscribe to destination.
func IsAllowedTopic(destination string) bool {
	dest := strings.TrimSpace(destination)
	if dest == "" {
		return false
	}
	switch dest {
	case TopicOverviewStats, TopicCanvasEvents, TopicApprovalsPending:
		return true
	}
	if strings.HasPrefix(dest, TopicConversationPrefix) && strings.HasSuffix(dest, ".updates") {
		return segmentCount(dest, ".") >= 3
	}
	if strings.HasPrefix(dest, TopicChannelPrefix) && strings.HasSuffix(dest, ".activity") {
		return segmentCount(dest, ".") >= 3
	}
	if strings.HasPrefix(dest, LegacyChannelMessagesPrefix) && strings.HasSuffix(dest, ".messages") {
		return segmentCount(dest, ".") >= 3
	}
	if strings.HasPrefix(dest, TopicHarnessPrefix) && strings.HasSuffix(dest, ".updates") {
		return segmentCount(dest, ".") >= 3
	}
	if strings.HasPrefix(dest, TopicProposalPrefix) && strings.HasSuffix(dest, ".updates") {
		return segmentCount(dest, ".") >= 3
	}
	return false
}

func segmentCount(s, sep string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, sep) + 1
}

// TopicsForView returns default STOMP topics for a view and optional context IDs.
type ViewContext struct {
	ChannelID  string
	PlanID     string
	SessionID  string
	ProposalID string
}

// TopicsForView lists topics a view should subscribe to when context is available.
func TopicsForView(view View, ctx ViewContext) []string {
	var topics []string
	switch view {
	case ViewHome:
		topics = append(topics, TopicOverviewStats, TopicApprovalsPending)
	case ViewDashboard:
		topics = append(topics, TopicOverviewStats, TopicCanvasEvents, TopicApprovalsPending)
	case ViewChannels:
		if ctx.ChannelID != "" {
			topics = append(topics, ChannelActivityTopic(ctx.ChannelID))
			if ctx.PlanID != "" {
				topics = append(topics, HarnessUpdatesTopic(ctx.PlanID))
			}
		}
	case ViewCourt:
		topics = append(topics, TopicApprovalsPending)
		if ctx.ProposalID != "" {
			topics = append(topics, ProposalUpdatesTopic(ctx.ProposalID))
		}
	case ViewCanvas:
		topics = append(topics, TopicCanvasEvents)
		if ctx.PlanID != "" {
			topics = append(topics, HarnessUpdatesTopic(ctx.PlanID))
		}
	case ViewTrace:
		if ctx.SessionID != "" {
			topics = append(topics, ConversationUpdatesTopic(ctx.SessionID))
		}
	}
	return topics
}

// ValidateViewTopics ensures subscribed topics match the view allow-list.
func ValidateViewTopics(view View, ctx ViewContext, subscribed []string) error {
	allowed := make(map[string]struct{})
	for _, t := range TopicsForView(view, ctx) {
		allowed[t] = struct{}{}
	}
	for _, topic := range subscribed {
		if !IsAllowedTopic(topic) {
			return fmt.Errorf("topic not allowed: %s", topic)
		}
		if len(allowed) > 0 {
			if _, ok := allowed[topic]; !ok {
				return fmt.Errorf("topic %s not permitted for view %s", topic, view)
			}
		}
	}
	return nil
}