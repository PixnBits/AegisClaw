package channelfacilitator

import (
	"strings"
	"time"

	"AegisClaw/internal/channeldata"
	"AegisClaw/internal/collab"
)

var assignmentPhrases = []string{
	"assign", "assigned", "please implement", "please review", "take ownership",
	"your task", "action item", "deliverable", "implement", "coder", "ciso",
	"flag", "concern", "push back", "security review",
}

// ComputeRelevanceAnchors selects up to maxAnchors prior message seqs using implicit signals.
func ComputeRelevanceAnchors(recipient string, sinceSeq int, batch []map[string]interface{}, window []map[string]interface{}, settings channeldata.TurnSettings) []int {
	maxAnchors := settings.MaxRelevanceAnchors
	if maxAnchors <= 0 {
		maxAnchors = channeldata.DefaultTurnSettings.MaxRelevanceAnchors
	}
	cutoff := relevanceWindowCutoff(window, settings)
	type scored struct {
		seq   int
		score int
	}
	minNewSeq := sinceSeq + 1
	for _, m := range batch {
		if seq := channeldata.MessageSeq(m); seq > 0 && (minNewSeq == sinceSeq+1 || seq < minNewSeq) {
			minNewSeq = seq
		}
	}
	scores := map[int]int{}
	for _, m := range window {
		seq := channeldata.MessageSeq(m)
		if seq <= 0 || seq >= minNewSeq {
			continue
		}
		if ts, ok := channeldata.MessageTimestamp(m); ok && ts.Before(cutoff) {
			continue
		}
		content := channeldata.MessageContent(m)
		from := channeldata.MessageFrom(m)
		s := 0
		if collab.IsMentioned(recipient, content) {
			s += 10
		}
		for _, b := range batch {
			if channeldata.MessageFrom(b) == from {
				s += 4
				break
			}
		}
		lower := strings.ToLower(content)
		for _, phrase := range assignmentPhrases {
			if strings.Contains(lower, phrase) {
				s += 3
				break
			}
		}
		if strings.HasPrefix(from, "project-manager") {
			s += 5
		}
		for _, b := range batch {
			if keywordOverlap(content, channeldata.MessageContent(b)) {
				s += 2
				break
			}
		}
		if s > 0 {
			scores[seq] = s
		}
	}
	var ranked []scored
	for seq, score := range scores {
		ranked = append(ranked, scored{seq: seq, score: score})
	}
	for i := 0; i < len(ranked); i++ {
		for j := i + 1; j < len(ranked); j++ {
			if ranked[j].score > ranked[i].score || (ranked[j].score == ranked[i].score && ranked[j].seq > ranked[i].seq) {
				ranked[i], ranked[j] = ranked[j], ranked[i]
			}
		}
	}
	out := make([]int, 0, maxAnchors)
	for _, r := range ranked {
		out = append(out, r.seq)
		if len(out) >= maxAnchors {
			break
		}
	}
	return out
}

func relevanceWindowCutoff(window []map[string]interface{}, settings channeldata.TurnSettings) time.Time {
	dur := settings.RelevanceWindowDuration
	if dur <= 0 {
		dur = channeldata.DefaultTurnSettings.RelevanceWindowDuration
	}
	cutoff := time.Now().UTC().Add(-dur)
	maxMsgs := settings.RelevanceWindowMessages
	if maxMsgs <= 0 {
		maxMsgs = channeldata.DefaultTurnSettings.RelevanceWindowMessages
	}
	if len(window) > maxMsgs {
		if ts, ok := channeldata.MessageTimestamp(window[len(window)-maxMsgs]); ok {
			if ts.After(cutoff) {
				cutoff = ts
			}
		}
	}
	return cutoff
}

func keywordOverlap(a, b string) bool {
	wordsA := tokenize(a)
	wordsB := tokenize(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}
	for w := range wordsA {
		if len(w) < 4 {
			continue
		}
		if wordsB[w] {
			return true
		}
	}
	return false
}

func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,!?;:\"'()[]{}")
		if w != "" {
			out[w] = true
		}
	}
	return out
}