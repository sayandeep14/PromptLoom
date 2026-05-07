package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/render"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/tokens"
)

// RunPlayground launches the interactive playground TUI for the named prompt.
func RunPlayground(name, cwd string) error {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}
	if _, ok := reg.LookupPrompt(name); !ok {
		return fmt.Errorf("prompt %q not found", name)
	}

	m := newPlaygroundModel(name, reg, cwd)
	m.cfg = cfg
	m.refresh()

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

type pgMode int

const (
	pgNormal    pgMode = iota
	pgPickVariant
	pgPickFormat
	pgPickOverlay
	pgInputContext // typing a context value
)

type playgroundModel struct {
	name    string
	reg     *registry.Registry
	cfg     interface{} // *config.Config — stored as interface{} to avoid import cycle
	cwd     string

	variant  string
	format   string
	overlays []string
	env      string

	variants  []string
	formats   []string
	overlayNames []string

	preview     string
	tokenCount  int
	hasContract bool
	renderErr   string

	scrollOffset int
	viewHeight   int
	viewWidth    int

	mode      pgMode
	pickCursor int

	contextInput textinput.Model
	copied       bool // show "Copied!" feedback
}

func newPlaygroundModel(name string, reg *registry.Registry, cwd string) playgroundModel {
	// Collect variants from the prompt node.
	variants := []string{"(none)"}
	if node, ok := reg.LookupPrompt(name); ok {
		for _, v := range node.Variants {
			variants = append(variants, v.Name)
		}
	}

	// Collect overlay names.
	overlayNames := []string{}
	for _, o := range reg.Overlays() {
		overlayNames = append(overlayNames, o.Name)
	}

	ti := textinput.New()
	ti.Placeholder = "e.g. prod  or  git:diff  or  file:src/main.go"
	ti.CharLimit = 80

	return playgroundModel{
		name:         name,
		reg:          reg,
		cwd:          cwd,
		variants:     variants,
		formats:      builtinFormats,
		overlayNames: overlayNames,
		contextInput: ti,
	}
}

// refresh re-renders the prompt with current settings.
func (m *playgroundModel) refresh() {
	cfgTyped, ok := m.cfg.(interface {
		GetRenderIncludeMetadata() bool
	})
	_ = cfgTyped
	_ = ok

	rp, err := resolve.ResolveWithOptions(m.name, m.reg, resolve.Options{
		Variant:  m.variant,
		Overlays: m.overlays,
		Env:      m.env,
	})
	if err != nil {
		m.renderErr = err.Error()
		m.preview = ""
		return
	}
	m.renderErr = ""
	m.hasContract = rp.AppliedVariant != "" || len(rp.InheritsChain) > 0

	// Use config from loader for proper rendering.
	reg2 := m.reg
	_ = reg2

	// Load actual config.
	_, cfg, _ := loader.Load(m.cwd)

	body, _, err := render.RenderFormat(rp, cfg, m.format)
	if err != nil {
		m.renderErr = err.Error()
		return
	}
	m.preview = body
	m.tokenCount = tokens.Estimate(body)
	// Detect contract.
	if node, ok := m.reg.LookupPrompt(m.name); ok {
		m.hasContract = node.Contract != nil
	}
}

func (m playgroundModel) Init() tea.Cmd {
	return nil
}

func (m playgroundModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.copied = false

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewWidth = msg.Width
		m.viewHeight = msg.Height

	case tea.KeyMsg:
		switch m.mode {
		case pgPickVariant:
			return m.updatePick(msg, m.variants, func(sel string) {
				if sel == "(none)" {
					m.variant = ""
				} else {
					m.variant = sel
				}
			})
		case pgPickFormat:
			return m.updatePick(msg, m.formats, func(sel string) {
				m.format = sel
			})
		case pgPickOverlay:
			return m.updatePick(msg, m.overlayNames, func(sel string) {
				for _, o := range m.overlays {
					if o == sel {
						return
					}
				}
				m.overlays = append(m.overlays, sel)
			})
		case pgInputContext:
			switch msg.String() {
			case "enter":
				m.env = strings.TrimSpace(m.contextInput.Value())
				m.contextInput.SetValue("")
				m.mode = pgNormal
				m.refresh()
			case "esc":
				m.mode = pgNormal
			default:
				var cmd tea.Cmd
				m.contextInput, cmd = m.contextInput.Update(msg)
				return m, cmd
			}
		default:
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "1":
				m.scrollOffset = 0
			case "2":
				CopyToClipboard(m.preview) //nolint:errcheck
				m.copied = true
			case "3":
				m.mode = pgPickVariant
				m.pickCursor = 0
			case "4":
				m.mode = pgPickFormat
				m.pickCursor = 0
			case "5":
				if len(m.overlayNames) > 0 {
					m.mode = pgPickOverlay
					m.pickCursor = 0
				}
			case "6":
				m.mode = pgInputContext
				m.contextInput.Focus()
			case "7":
				m.overlays = nil
				m.env = ""
				m.refresh()
			case "8":
				m.saveToDistDir()
			case "up", "k":
				if m.scrollOffset > 0 {
					m.scrollOffset--
				}
			case "down", "j":
				previewLines := strings.Count(m.preview, "\n")
				maxScroll := previewLines - m.previewHeight() + 2
				if maxScroll < 0 {
					maxScroll = 0
				}
				if m.scrollOffset < maxScroll {
					m.scrollOffset++
				}
			}
		}
	}
	return m, nil
}

func (m playgroundModel) updatePick(msg tea.KeyMsg, items []string, onSelect func(string)) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.pickCursor > 0 {
			m.pickCursor--
		}
	case "down", "j":
		if m.pickCursor < len(items)-1 {
			m.pickCursor++
		}
	case "enter":
		if m.pickCursor < len(items) {
			onSelect(items[m.pickCursor])
		}
		m.mode = pgNormal
		m.refresh()
	case "esc", "q":
		m.mode = pgNormal
	}
	return m, nil
}

func (m *playgroundModel) saveToDistDir() {
	if m.preview == "" {
		return
	}
	out := filepath.Join(m.cwd, "dist", "prompts", m.name+".md")
	_ = os.MkdirAll(filepath.Dir(out), 0o755)
	_ = os.WriteFile(out, []byte(m.preview), 0o644)
}

func (m playgroundModel) previewHeight() int {
	h := m.viewHeight - 16 // header + controls + stats
	if h < 5 {
		h = 5
	}
	return h
}

func (m playgroundModel) View() string {
	if m.viewWidth == 0 {
		return "loading…"
	}

	var b strings.Builder
	width := m.viewWidth
	if width > 100 {
		width = 100
	}

	// ── header ──
	variantStr := m.variant
	if variantStr == "" {
		variantStr = "(none)"
	}
	envStr := m.env
	if envStr == "" {
		envStr = "(none)"
	}
	overlayStr := strings.Join(m.overlays, ", ")
	if overlayStr == "" {
		overlayStr = "(none)"
	}

	border := MutedStyle.Render("─")
	topBorder := "╭" + strings.Repeat("─", width-2) + "╮"
	botBorder := "╰" + strings.Repeat("─", width-2) + "╯"

	b.WriteString(BannerStyle.Render(topBorder) + "\n")
	title := fmt.Sprintf(" Playground: %s ", m.name)
	padding := strings.Repeat(" ", width-2-len(title))
	b.WriteString(BannerStyle.Render("│") + BannerStyle.Render(title) + MutedStyle.Render(padding) + BannerStyle.Render("│") + "\n")

	row1 := fmt.Sprintf(" Variant: %-12s Profile: %-10s Env: %-12s",
		variantStr, "(none)", envStr)
	row2 := fmt.Sprintf(" Overlays: %-50s", overlayStr)
	pad1 := strings.Repeat(" ", max2(0, width-2-len(row1)))
	pad2 := strings.Repeat(" ", max2(0, width-2-len(row2)))
	b.WriteString(BannerStyle.Render("│") + row1 + pad1 + BannerStyle.Render("│") + "\n")
	b.WriteString(BannerStyle.Render("│") + row2 + pad2 + BannerStyle.Render("│") + "\n")
	b.WriteString(BannerStyle.Render(botBorder) + "\n")
	b.WriteByte('\n')

	// ── pick overlay ──
	if m.mode == pgPickOverlay || m.mode == pgPickVariant || m.mode == pgPickFormat {
		var items []string
		var header string
		switch m.mode {
		case pgPickVariant:
			items = m.variants
			header = "Choose variant:"
		case pgPickFormat:
			items = m.formats
			header = "Choose format:"
		case pgPickOverlay:
			items = m.overlayNames
			header = "Choose overlay to add:"
		}
		b.WriteString("  " + SubHeaderStyle.Render(header) + "\n\n")
		for i, item := range items {
			cur := "  "
			if i == m.pickCursor {
				cur = SuccessStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", cur, item))
		}
		b.WriteString("\n  " + MutedStyle.Render("↑↓ navigate  Enter select  Esc cancel") + "\n")
		return b.String()
	}

	if m.mode == pgInputContext {
		b.WriteString("  " + SubHeaderStyle.Render("Set env block name (e.g. prod, staging):") + "\n\n")
		b.WriteString("  " + m.contextInput.View() + "\n\n")
		b.WriteString("  " + MutedStyle.Render("Enter to apply  Esc to cancel") + "\n")
		return b.String()
	}

	// ── actions ──
	copied := ""
	if m.copied {
		copied = "  " + SuccessStyle.Render("Copied!")
	}
	actions := []string{
		CommandStyle.Render("[1]") + " Preview ↑↓",
		CommandStyle.Render("[2]") + " Copy" + copied,
		CommandStyle.Render("[3]") + " Variant",
		CommandStyle.Render("[4]") + " Format",
		CommandStyle.Render("[5]") + " Overlay",
		CommandStyle.Render("[6]") + " Env",
		CommandStyle.Render("[7]") + " Reset",
		CommandStyle.Render("[8]") + " Save",
	}
	b.WriteString("  " + strings.Join(actions[:4], "   ") + "\n")
	b.WriteString("  " + strings.Join(actions[4:], "   ") + "\n\n")
	b.WriteString("  " + MutedStyle.Render(strings.Repeat(border, width-4)) + "\n\n")

	// ── preview ──
	if m.renderErr != "" {
		b.WriteString("  " + ErrorStyle.Render("Error: "+m.renderErr) + "\n")
	} else {
		lines := strings.Split(m.preview, "\n")
		end := m.scrollOffset + m.previewHeight()
		if end > len(lines) {
			end = len(lines)
		}
		visible := lines
		if m.scrollOffset < len(lines) {
			visible = lines[m.scrollOffset:end]
		}
		for _, l := range visible {
			if len(l) > width-4 {
				l = l[:width-4]
			}
			b.WriteString("  " + l + "\n")
		}
	}

	// ── stats bar ──
	contractStr := MutedStyle.Render("no contract")
	if m.hasContract {
		contractStr = SuccessStyle.Render("contract: declared")
	}
	fmtStr := m.format
	if fmtStr == "" {
		fmtStr = "markdown"
	}
	b.WriteString("\n  " + MutedStyle.Render(strings.Repeat(border, width-4)) + "\n")
	stats := fmt.Sprintf("  Token estimate: %s  •  Format: %s  •  %s  •  q quit",
		PromptNameStyle.Render(fmt.Sprintf("%d", m.tokenCount)),
		MutedStyle.Render(fmtStr),
		contractStr)
	b.WriteString(stats + "\n")

	return b.String()
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
