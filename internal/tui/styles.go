package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — Catppuccin-inspired with AegisClaw branding.
var (
	ColorPrimary   = lipgloss.Color("#89b4fa") // Blue
	ColorSecondary = lipgloss.Color("#a6e3a1") // Green
	ColorWarning   = lipgloss.Color("#f9e2af") // Yellow
	ColorDanger    = lipgloss.Color("#f38ba8") // Red/Pink
	ColorMuted     = lipgloss.Color("#6c7086") // Gray
	ColorAccent    = lipgloss.Color("#cba6f7") // Lavender
	ColorSurface   = lipgloss.Color("#313244") // Dark surface
	ColorBase      = lipgloss.Color("#1e1e2e") // Base dark
	ColorText      = lipgloss.Color("#cdd6f4") // Light text
)

// Shared layout styles.
var (
	// TitleStyle renders section titles.
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			MarginBottom(1)

	// SubtitleStyle renders section subtitles.
	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorAccent).
			Italic(true)

	// HeaderStyle renders table/panel headers.
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(ColorSurface).
			Padding(0, 1)

	// SelectedStyle highlights the currently focused row.
	SelectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary)

	// MutedStyle for less important content.
	MutedStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// StatusApproved renders approved/success states.
	StatusApproved = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSecondary)

	// StatusRejected renders rejected/error states.
	StatusRejected = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorDanger)

	// StatusPending renders pending/in-progress states.
	StatusPending = lipgloss.NewStyle().
			Foreground(ColorWarning)

	// StatusDraft renders draft states.
	StatusDraft = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// BorderStyle renders a bordered panel.
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorMuted).
			Padding(1, 2)

	// HelpStyle renders the help bar at the bottom.
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)

	// ErrorStyle renders error messages.
	ErrorStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorDanger)

	// BadgeStyle renders inline status badges.
	BadgeStyle = lipgloss.NewStyle().
			Padding(0, 1)
)

// RiskStyle returns the appropriate style for a risk level.
func RiskStyle(risk string) lipgloss.Style {
	switch risk {
	case "low":
		return lipgloss.NewStyle().Foreground(ColorSecondary)
	case "medium":
		return lipgloss.NewStyle().Foreground(ColorWarning)
	case "high":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#fab387"))
	case "critical":
		return lipgloss.NewStyle().Bold(true).Foreground(ColorDanger)
	default:
		return MutedStyle
	}
}

// StatusStyle returns the appropriate style for a proposal/session status.
func StatusStyle(status string) lipgloss.Style {
	switch status {
	case "approved", "complete", "consensus":
		return StatusApproved
	case "rejected", "failed", "error":
		return StatusRejected
	case "in_review", "reviewing", "submitted", "implementing":
		return StatusPending
	case "draft", "pending":
		return StatusDraft
	default:
		return MutedStyle
	}
}

// SandboxStateStyle returns the appropriate style for sandbox states.
func SandboxStateStyle(state string) lipgloss.Style {
	switch state {
	case "running", "active":
		return StatusApproved
	case "stopped", "inactive":
		return StatusDraft
	case "error":
		return StatusRejected
	case "created":
		return StatusPending
	default:
		return MutedStyle
	}
}
