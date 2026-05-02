package tui

import "github.com/charmbracelet/lipgloss"

// Palette vars — set by ApplyTheme(), initialized via init().
var (
	clrPrimary   lipgloss.Color
	clrAccent    lipgloss.Color
	clrSuccess   lipgloss.Color
	clrWarning   lipgloss.Color
	clrError     lipgloss.Color
	clrMuted     lipgloss.Color
	clrText      lipgloss.Color
	clrBright    lipgloss.Color
	clrDim       lipgloss.Color
	clrHighlight lipgloss.Color
)

// Exported styles used by CLI commands and the REPL.
// All are uninitialized at declaration; ApplyTheme() fills them.
var (
	BannerStyle        lipgloss.Style
	TaglineStyle       lipgloss.Style
	VersionStyle       lipgloss.Style
	SuccessStyle       lipgloss.Style
	ErrorStyle         lipgloss.Style
	WarningStyle       lipgloss.Style
	MutedStyle         lipgloss.Style
	BrightStyle        lipgloss.Style
	TextStyle          lipgloss.Style
	HeaderStyle        lipgloss.Style
	SubHeaderStyle     lipgloss.Style
	PathStyle          lipgloss.Style
	PromptNameStyle    lipgloss.Style
	FocusedPromptStyle lipgloss.Style
	BlockNameStyle     lipgloss.Style
	InheritsStyle      lipgloss.Style
	CommandStyle       lipgloss.Style
	ArgDescStyle       lipgloss.Style
	DividerStyle       lipgloss.Style
	SelectedCompStyle  lipgloss.Style
	UnselectedCompStyle lipgloss.Style
	InputPromptStyle   lipgloss.Style
	SummaryBox         lipgloss.Style
	BulletStyle        lipgloss.Style

	// TraceArrow separates steps in a field resolution chain.
	TraceArrow string
)

// ApplyTheme rebuilds all color vars and style vars from activeTheme.
// Call this after changing activeTheme.
func ApplyTheme() {
	t := activeTheme

	// Update palette vars.
	clrPrimary = t.Primary
	clrAccent = t.Accent
	clrSuccess = t.Success
	clrWarning = t.Warning
	clrError = t.Error
	clrMuted = t.Muted
	clrText = t.Text
	clrBright = t.Bright
	clrDim = t.Dim
	clrHighlight = t.Highlight

	// Rebuild all styles.
	BannerStyle = lipgloss.NewStyle().Foreground(clrPrimary).Bold(true)
	TaglineStyle = lipgloss.NewStyle().Foreground(clrMuted).Italic(true)
	VersionStyle = lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	SuccessStyle = lipgloss.NewStyle().Foreground(clrSuccess).Bold(true)
	ErrorStyle = lipgloss.NewStyle().Foreground(clrError).Bold(true)
	WarningStyle = lipgloss.NewStyle().Foreground(clrWarning)
	MutedStyle = lipgloss.NewStyle().Foreground(clrMuted)
	BrightStyle = lipgloss.NewStyle().Foreground(clrBright)
	TextStyle = lipgloss.NewStyle().Foreground(clrText)
	HeaderStyle = lipgloss.NewStyle().Foreground(clrPrimary).Bold(true)
	SubHeaderStyle = lipgloss.NewStyle().Foreground(clrBright).Bold(true)
	PathStyle = lipgloss.NewStyle().Foreground(clrAccent)
	PromptNameStyle = lipgloss.NewStyle().Foreground(clrPrimary).Bold(true)

	FocusedPromptStyle = lipgloss.NewStyle().
		Background(clrHighlight).
		Foreground(t.HighlightText).
		Bold(true).
		Padding(0, 1)

	BlockNameStyle = lipgloss.NewStyle().Foreground(clrAccent)
	InheritsStyle = lipgloss.NewStyle().Foreground(clrMuted)
	CommandStyle = lipgloss.NewStyle().Foreground(clrBright).Bold(true)
	ArgDescStyle = lipgloss.NewStyle().Foreground(clrMuted)
	DividerStyle = lipgloss.NewStyle().Foreground(clrDim)

	SelectedCompStyle = lipgloss.NewStyle().
		Background(clrHighlight).
		Foreground(t.HighlightText).
		Padding(0, 1)

	UnselectedCompStyle = lipgloss.NewStyle().
		Foreground(clrMuted).
		Padding(0, 1)

	InputPromptStyle = lipgloss.NewStyle().Foreground(clrPrimary).Bold(true)

	SummaryBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(clrDim).
		Padding(0, 2)

	BulletStyle = lipgloss.NewStyle().Foreground(clrPrimary)

	// Derived string constants.
	TraceArrow = MutedStyle.Render(" → ")
}

func init() {
	ApplyTheme()
}

// Divider returns a styled horizontal rule of n dashes.
func Divider(n int) string {
	line := ""
	for i := 0; i < n; i++ {
		line += "─"
	}
	return DividerStyle.Render(line)
}
