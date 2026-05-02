package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds all color tokens for a visual theme.
type Theme struct {
	Name          string
	Primary       lipgloss.Color
	Accent        lipgloss.Color
	Success       lipgloss.Color
	Warning       lipgloss.Color
	Error         lipgloss.Color
	Muted         lipgloss.Color
	Text          lipgloss.Color
	Bright        lipgloss.Color
	Dim           lipgloss.Color
	Highlight     lipgloss.Color
	HighlightText lipgloss.Color
}

// DarkTheme is the default dark color scheme.
var DarkTheme = Theme{
	Name:          "dark",
	Primary:       lipgloss.Color("#A78BFA"),
	Accent:        lipgloss.Color("#F59E0B"),
	Success:       lipgloss.Color("#34D399"),
	Warning:       lipgloss.Color("#FBBF24"),
	Error:         lipgloss.Color("#F87171"),
	Muted:         lipgloss.Color("#6B7280"),
	Text:          lipgloss.Color("#E2E8F0"),
	Bright:        lipgloss.Color("#C4B5FD"),
	Dim:           lipgloss.Color("#374151"),
	Highlight:     lipgloss.Color("#7C3AED"),
	HighlightText: lipgloss.Color("#E2E8F0"),
}

// LightTheme is a cyan/teal palette — distinct from the default violet/amber
// and readable on both light and dark terminals.
var LightTheme = Theme{
	Name:          "light",
	Primary:       lipgloss.Color("#22D3EE"), // sky cyan
	Accent:        lipgloss.Color("#34D399"), // emerald
	Success:       lipgloss.Color("#4ADE80"), // green
	Warning:       lipgloss.Color("#FDE68A"), // amber
	Error:         lipgloss.Color("#F87171"), // red (same as dark)
	Muted:         lipgloss.Color("#94A3B8"), // slate
	Text:          lipgloss.Color("#E2E8F0"), // near-white (same as dark)
	Bright:        lipgloss.Color("#67E8F9"), // bright cyan
	Dim:           lipgloss.Color("#334155"), // dark slate
	Highlight:     lipgloss.Color("#0E7490"), // deep teal
	HighlightText: lipgloss.Color("#F0FDFA"), // near-white on teal bg
}

var activeTheme = DarkTheme

// SetTheme switches the active theme by name ("dark" or "light").
// Returns false if the name is unrecognized.
func SetTheme(name string) bool {
	switch strings.ToLower(name) {
	case "light":
		activeTheme = LightTheme
		ApplyTheme()
		return true
	case "dark":
		activeTheme = DarkTheme
		ApplyTheme()
		return true
	}
	return false
}

// CurrentThemeName returns the name of the active theme.
func CurrentThemeName() string {
	return activeTheme.Name
}
