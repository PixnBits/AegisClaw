package court

import (
	"fmt"
	"strings"

	"github.com/PixnBits/AegisClaw/internal/proposal"
)

// ConsensusResult holds the result of a weighted consensus evaluation.
type ConsensusResult struct {
	Reached       bool               `json:"reached"`
	WeightedScore float64            `json:"weighted_score"`
	ApprovalRate  float64            `json:"approval_rate"`
	RejectRate    float64            `json:"reject_rate"`
	AskRate       float64            `json:"ask_rate"`
	AvgRisk       float64            `json:"avg_risk"`
	Heatmap       map[string]float64 `json:"heatmap"`
	Feedback      IterationFeedback  `json:"feedback"`
}

// IterationFeedback collects questions and concerns from "ask" verdicts
// to be fed into the next round of review.
type IterationFeedback struct {
	Questions    []string `json:"questions,omitempty"`
	Concerns     []string `json:"concerns,omitempty"`
	RoundNumber  int      `json:"round_number"`
	HasQuestions bool     `json:"has_questions"`
}

// FormatFeedbackPrompt creates a prompt supplement for the next round
// containing questions and concerns from the previous round.
func (f *IterationFeedback) FormatFeedbackPrompt() string {
	if !f.HasQuestions && len(f.Concerns) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("\n--- Feedback from Round %d ---\n", f.RoundNumber))

	if len(f.Questions) > 0 {
		b.WriteString("Unanswered questions from prior reviewers:\n")
		for i, q := range f.Questions {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, q))
		}
	}

	if len(f.Concerns) > 0 {
		b.WriteString("Reviewer concerns:\n")
		for i, c := range f.Concerns {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, c))
		}
	}

	b.WriteString("Please address these in your review.\n")
	return b.String()
}

// EvaluateConsensus performs weighted consensus evaluation using persona weights.
// Reviews with "ask" or "abstain" verdicts reduce the effective quorum denominator,
// and their questions are collected as feedback for subsequent rounds.
func EvaluateConsensus(reviews []proposal.Review, personas []*Persona, quorum float64) *ConsensusResult {
	if len(reviews) == 0 {
		return &ConsensusResult{
			Heatmap: make(map[string]float64),
		}
	}

	// Build persona weight map
	weights := make(map[string]float64)
	for _, p := range personas {
		weights[p.Name] = p.Weight
	}

	var totalWeight, approveWeight, rejectWeight, askWeight float64
	var totalRisk float64
	heatmap := make(map[string]float64)
	var questions []string
	var concerns []string

	for _, r := range reviews {
		w := weights[r.Persona]
		if w <= 0 {
			// Equal weight fallback for unknown personas
			w = 1.0 / float64(len(reviews))
		}
		totalWeight += w
		totalRisk += r.RiskScore
		heatmap[r.Persona] = r.RiskScore

		switch r.Verdict {
		case proposal.VerdictApprove:
			approveWeight += w
		case proposal.VerdictReject:
			rejectWeight += w
			// Rejection comments are treated as concerns
			if r.Comments != "" {
				concerns = append(concerns, fmt.Sprintf("[%s] %s", r.Persona, r.Comments))
			}
		case proposal.VerdictAsk:
			askWeight += w
			questions = append(questions, r.Questions...)
			if r.Comments != "" {
				concerns = append(concerns, fmt.Sprintf("[%s] %s", r.Persona, r.Comments))
			}
		case proposal.VerdictAbstain:
			// Abstained weight is still counted in total, but does not contribute
		}
	}

	avgRisk := totalRisk / float64(len(reviews))

	approvalRate := 0.0
	rejectRate := 0.0
	askRate := 0.0
	if totalWeight > 0 {
		approvalRate = approveWeight / totalWeight
		rejectRate = rejectWeight / totalWeight
		askRate = askWeight / totalWeight
	}

	// Consensus is reached when weighted approval rate meets or exceeds quorum
	reached := approvalRate >= quorum

	return &ConsensusResult{
		Reached:       reached,
		WeightedScore: approveWeight,
		ApprovalRate:  approvalRate,
		RejectRate:    rejectRate,
		AskRate:       askRate,
		AvgRisk:       avgRisk,
		Heatmap:       heatmap,
		Feedback: IterationFeedback{
			Questions:    questions,
			Concerns:     concerns,
			HasQuestions:  len(questions) > 0,
		},
	}
}
