package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/registry"
)

// RunInteractiveWeave launches the guided prompt assembly wizard.
func RunInteractiveWeave(cwd string) error {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return fmt.Errorf("load project: %w", err)
	}

	m := newWizardModel(reg, cwd)
	p := tea.NewProgram(m, tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return err
	}
	if wm, ok := result.(wizardModel); ok && wm.written != "" {
		fmt.Println()
		fmt.Println("  " + SuccessStyle.Render("Created "+wm.written))
		fmt.Println("  " + MutedStyle.Render("Run: loom weave "+wm.promptName+"  or  loom inspect"))
		fmt.Println()
	}
	return nil
}

type wizardStep int

const (
	stepBase     wizardStep = iota // choose base prompt
	stepBlocks                     // choose blocks
	stepVariant                    // choose variant
	stepFormat                     // choose output format
	stepName                       // enter prompt name
	stepDone                       // done
	stepCancelled                  // user quit
)

type wizardModel struct {
	step      wizardStep
	reg       *registry.Registry
	cwd       string

	// choices
	prompts  []string // available prompt names
	blocks   []string // available block names
	variants []string // available variant names (from selected base)
	formats  []string

	// selections
	baseName  string
	useBlocks []string
	variant   string
	format    string

	// name input
	nameInput textinput.Model

	// cursor positions per step
	cursor [5]int
	// multi-select toggle state for blocks step
	blockSelected []bool

	// output
	promptName string
	written    string

	width  int
	height int
}

var builtinFormats = []string{"markdown", "json-anthropic", "json-openai", "cursor-rule", "copilot", "claude-code", "plain"}

func newWizardModel(reg *registry.Registry, cwd string) wizardModel {
	prompts := []string{"(none)"}
	for _, n := range reg.Prompts() {
		prompts = append(prompts, n.Name)
	}
	blocks := []string{}
	for _, b := range reg.Blocks() {
		blocks = append(blocks, b.Name)
	}

	ti := textinput.New()
	ti.Placeholder = "MyPrompt"
	ti.Focus()
	ti.CharLimit = 60

	return wizardModel{
		step:          stepBase,
		reg:           reg,
		cwd:           cwd,
		prompts:       prompts,
		blocks:        blocks,
		blockSelected: make([]bool, len(blocks)),
		formats:       builtinFormats,
		nameInput:     ti,
	}
}

func (m wizardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m wizardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.step == stepName {
				// let textinput handle q
			} else {
				m.step = stepCancelled
				return m, tea.Quit
			}

		case "up", "k":
			if m.step != stepName {
				if m.cursor[m.step] > 0 {
					m.cursor[m.step]--
				}
			}

		case "down", "j":
			if m.step != stepName {
				max := m.listLen() - 1
				if m.cursor[m.step] < max {
					m.cursor[m.step]++
				}
			}

		case " ":
			if m.step == stepBlocks && len(m.blocks) > 0 {
				idx := m.cursor[stepBlocks]
				m.blockSelected[idx] = !m.blockSelected[idx]
			}

		case "enter":
			return m.confirm()

		case "esc":
			if m.step > stepBase {
				m.step--
			}
		}
	}

	if m.step == stepName {
		var cmd tea.Cmd
		m.nameInput, cmd = m.nameInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m wizardModel) listLen() int {
	switch m.step {
	case stepBase:
		return len(m.prompts)
	case stepBlocks:
		if len(m.blocks) == 0 {
			return 1 // "continue" option
		}
		return len(m.blocks) + 1 // +1 for "continue" at bottom
	case stepVariant:
		return len(m.variants)
	case stepFormat:
		return len(m.formats)
	}
	return 0
}

func (m wizardModel) confirm() (tea.Model, tea.Cmd) {
	switch m.step {
	case stepBase:
		sel := m.prompts[m.cursor[stepBase]]
		if sel == "(none)" {
			m.baseName = ""
		} else {
			m.baseName = sel
			// populate variants from selected base
			if node, ok := m.reg.LookupPrompt(sel); ok {
				m.variants = []string{"(none)"}
				for _, v := range node.Variants {
					m.variants = append(m.variants, v.Name)
				}
			}
		}
		if len(m.variants) == 0 {
			m.variants = []string{"(none)"}
		}
		if len(m.blocks) == 0 {
			m.step = stepVariant
		} else {
			m.step = stepBlocks
		}

	case stepBlocks:
		// collect selected blocks
		m.useBlocks = nil
		for i, sel := range m.blockSelected {
			if sel {
				m.useBlocks = append(m.useBlocks, m.blocks[i])
			}
		}
		m.step = stepVariant

	case stepVariant:
		v := m.variants[m.cursor[stepVariant]]
		if v == "(none)" {
			m.variant = ""
		} else {
			m.variant = v
		}
		m.step = stepFormat

	case stepFormat:
		m.format = m.formats[m.cursor[stepFormat]]
		m.step = stepName
		m.nameInput.Focus()

	case stepName:
		name := strings.TrimSpace(m.nameInput.Value())
		if name == "" {
			return m, nil
		}
		m.promptName = name
		if err := m.writePromptFile(); err == nil {
			m.step = stepDone
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *wizardModel) writePromptFile() error {
	var b strings.Builder
	b.WriteString("prompt " + m.promptName)
	if m.baseName != "" {
		b.WriteString(" inherits " + m.baseName)
	}
	b.WriteString(" {\n")
	for _, bl := range m.useBlocks {
		b.WriteString("  use " + bl + "\n")
	}
	if len(m.useBlocks) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString("  persona:\n    You are a helpful assistant.\n\n")
	b.WriteString("  instructions:\n    - Respond clearly and concisely.\n\n")
	if m.variant != "" {
		b.WriteString("  variant " + m.variant + " {\n  }\n\n")
	}
	b.WriteString("}\n")

	outDir := filepath.Join(m.cwd, "prompts")
	_ = os.MkdirAll(outDir, 0o755)
	outPath := filepath.Join(outDir, m.promptName+".prompt.loom")
	if err := os.WriteFile(outPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	m.written = outPath
	return nil
}

func (m wizardModel) View() string {
	if m.step == stepCancelled {
		return ""
	}
	if m.step == stepDone {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("  " + BannerStyle.Render("loom weave --interactive") + "\n\n")

	// Progress
	steps := []string{"Base", "Blocks", "Variant", "Format", "Name"}
	var prog []string
	for i, s := range steps {
		if wizardStep(i) < m.step {
			prog = append(prog, SuccessStyle.Render("✓ "+s))
		} else if wizardStep(i) == m.step {
			prog = append(prog, PromptNameStyle.Render("▶ "+s))
		} else {
			prog = append(prog, MutedStyle.Render("· "+s))
		}
	}
	b.WriteString("  " + strings.Join(prog, MutedStyle.Render("  →  ")) + "\n\n")
	b.WriteString("  " + MutedStyle.Render(Divider(60)) + "\n\n")

	switch m.step {
	case stepBase:
		b.WriteString("  " + SubHeaderStyle.Render("Choose a base prompt to inherit from:") + "\n\n")
		for i, p := range m.prompts {
			cursor := "  "
			if i == m.cursor[stepBase] {
				cursor = SuccessStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", cursor, p))
		}

	case stepBlocks:
		b.WriteString("  " + SubHeaderStyle.Render("Choose blocks to include (Space to toggle, Enter to continue):") + "\n\n")
		for i, bl := range m.blocks {
			cursor := "  "
			if i == m.cursor[stepBlocks] {
				cursor = SuccessStyle.Render("▶ ")
			}
			check := "[ ]"
			if m.blockSelected[i] {
				check = SuccessStyle.Render("[x]")
			}
			b.WriteString(fmt.Sprintf("  %s%s %s\n", cursor, check, bl))
		}
		b.WriteString("\n  " + MutedStyle.Render("Press Enter to continue →") + "\n")

	case stepVariant:
		b.WriteString("  " + SubHeaderStyle.Render("Choose a variant:") + "\n\n")
		for i, v := range m.variants {
			cursor := "  "
			if i == m.cursor[stepVariant] {
				cursor = SuccessStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", cursor, v))
		}

	case stepFormat:
		b.WriteString("  " + SubHeaderStyle.Render("Choose output format:") + "\n\n")
		for i, f := range m.formats {
			cursor := "  "
			if i == m.cursor[stepFormat] {
				cursor = SuccessStyle.Render("▶ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s\n", cursor, f))
		}

	case stepName:
		b.WriteString("  " + SubHeaderStyle.Render("Prompt name:") + "\n\n")
		b.WriteString("  " + m.nameInput.View() + "\n\n")
		if m.baseName != "" {
			b.WriteString("  " + MutedStyle.Render("inherits: "+m.baseName) + "\n")
		}
		if len(m.useBlocks) > 0 {
			b.WriteString("  " + MutedStyle.Render("uses: "+strings.Join(m.useBlocks, ", ")) + "\n")
		}
		b.WriteString("  " + MutedStyle.Render("format: "+m.format) + "\n")
	}

	b.WriteString("\n  " + MutedStyle.Render("↑↓ navigate  Enter confirm  Esc back  q quit") + "\n")
	return b.String()
}
