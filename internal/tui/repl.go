package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sayandeepgiri/promptloom/internal/config"
)

// cmdResultMsg carries the output of an executed command back to the model.
type cmdResultMsg struct {
	output string
	isErr  bool
}

// bannerRevealMsg triggers the next tick of the banner reveal animation.
type bannerRevealMsg struct{}

// bannerGlowMsg triggers one pulse-glow frame after the reveal completes.
type bannerGlowMsg struct{}

func bannerRevealCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(18 * time.Millisecond)
		return bannerRevealMsg{}
	}
}

func bannerGlowCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(130 * time.Millisecond)
		return bannerGlowMsg{}
	}
}

// replModel is the bubbletea model for the interactive REPL.
type replModel struct {
	input      textinput.Model
	history    []string
	histIdx    int    // -1 = live; >= 0 = browsing history
	savedInput string // saved while browsing history

	// Tab completions (command names).
	completions []string
	compIdx     int
	compActive  bool

	// # prompt picker.
	pickerActive   bool
	pickerAll      []PickerItem // full unfiltered list
	pickerFiltered []PickerItem // filtered by current filter text
	pickerIdx      int          // selected index in pickerFiltered
	pickerHashByte int          // byte offset of '#' in input value

	outputLines []string

	cwd          string
	promptNames  []string
	bannerString string

	// Banner reveal animation.
	bannerLines      int // how many lines of the banner to show
	bannerTotalLines int // total lines in banner string
	bannerGlowTick   int // 0 = idle; 1–4 = active pulse glow frames

	// Spinner for running commands.
	spinner    spinner.Model
	running    bool
	runningCmd string

	// Store version for banner rebuilds after theme change.
	bannerVersion string

	commandsHidden bool

	width  int
	height int
}

// Run starts the interactive REPL.
func Run(version, cwd string) error {
	p, b, e := LibraryStats(cwd)
	banner := Banner(version, cwd, p, b, e, true)
	names := PromptNames(cwd)

	ti := textinput.New()
	ti.Placeholder = "type a command…  # to pick a prompt"
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 60
	ti.PromptStyle = InputPromptStyle
	ti.Prompt = "❯ "

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(clrPrimary)

	m := replModel{
		input:            ti,
		histIdx:          -1,
		compIdx:          -1,
		pickerIdx:        0,
		cwd:              cwd,
		promptNames:      names,
		bannerString:     banner,
		bannerTotalLines: strings.Count(banner, "\n"),
		bannerLines:      0,
		spinner:          sp,
		bannerVersion:    version,
		pickerAll:        BuildPickerItems(cwd),
	}

	prog := tea.NewProgram(m, tea.WithAltScreen())
	_, err := prog.Run()
	return err
}

func (m replModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, bannerRevealCmd())
}

func (m replModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case bannerRevealMsg:
		if m.bannerLines < m.bannerTotalLines {
			m.bannerLines++
			return m, bannerRevealCmd()
		}
		// Reveal complete — start the settle-pulse glow.
		m.bannerGlowTick = 4
		return m, bannerGlowCmd()

	case bannerGlowMsg:
		if m.bannerGlowTick > 0 {
			m.bannerGlowTick--
			return m, bannerGlowCmd()
		}
		return m, nil

	case spinner.TickMsg:
		if m.running {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case cmdResultMsg:
		m.running = false
		lines := strings.Split(strings.TrimRight(msg.output, "\n"), "\n")
		m.outputLines = append(m.outputLines, lines...)
		const maxLines = 400
		if len(m.outputLines) > maxLines {
			m.outputLines = m.outputLines[len(m.outputLines)-maxLines:]
		}
		return m, nil

	case tea.KeyMsg:
		// ── Global quit ───────────────────────────────────────────────────
		if msg.Type == tea.KeyCtrlC {
			m.input.Blur()
			return m, tea.Quit
		}

		// ── Picker active ──────────────────────────────────────────────────
		if m.pickerActive {
			switch msg.Type {
			case tea.KeyEnter:
				if len(m.pickerFiltered) > 0 {
					m = m.applyPickerSelection()
				}
				return m, nil

			case tea.KeyEscape:
				m = m.closePicker(true)
				return m, nil

			case tea.KeyUp:
				if m.pickerIdx > 0 {
					m.pickerIdx--
				}
				return m, nil

			case tea.KeyDown:
				if m.pickerIdx < len(m.pickerFiltered)-1 {
					m.pickerIdx++
				}
				return m, nil

			default:
				// All other keys go to the text input, then we re-sync picker.
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m = m.syncPicker()
				return m, cmd
			}
		}

		// ── Normal mode ────────────────────────────────────────────────────
		switch msg.Type {

		case tea.KeyEnter:
			raw := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			m.compActive = false
			m.compIdx = -1
			m.completions = nil
			m.histIdx = -1

			if raw == "" {
				return m, nil
			}
			if len(m.history) == 0 || m.history[len(m.history)-1] != raw {
				m.history = append(m.history, raw)
			}
			if raw == "exit" || raw == "quit" || raw == "q" {
				return m, tea.Quit
			}
			if raw == "clear" {
				m.outputLines = nil
				return m, nil
			}
			m.outputLines = append(m.outputLines,
				InputPromptStyle.Render("❯")+" "+BrightStyle.Render(raw))

			// Handle theme switching inline — needs banner rebuild and style refresh.
			if strings.HasPrefix(raw, "theme") {
				parts := strings.Fields(raw)
				out, isErr := dispatch("theme", parts[1:], m.cwd)
				lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
				m.outputLines = append(m.outputLines, lines...)
				if !isErr && len(parts) >= 2 {
					p, b, e := LibraryStats(m.cwd)
					m.bannerString = Banner(m.bannerVersion, m.cwd, p, b, e, !m.commandsHidden)
					m.bannerTotalLines = strings.Count(m.bannerString, "\n")
					m.bannerLines = 0 // replay reveal with new colors
					m.bannerGlowTick = 0
					// Propagate new styles to live components.
					m.input.PromptStyle = InputPromptStyle
					m.input.TextStyle = TextStyle
					m.spinner.Style = lipgloss.NewStyle().Foreground(clrPrimary)
					return m, bannerRevealCmd()
				}
				return m, nil
			}

			// Handle hide/show inline — toggles the command table in the banner.
			if raw == "hide" || raw == "show" {
				m.commandsHidden = raw == "hide"
				p, b, e := LibraryStats(m.cwd)
				m.bannerString = Banner(m.bannerVersion, m.cwd, p, b, e, !m.commandsHidden)
				m.bannerTotalLines = strings.Count(m.bannerString, "\n")
				m.bannerLines = m.bannerTotalLines // no re-animation, instant swap
				m.bannerGlowTick = 0
				return m, nil
			}

			// graph (ascii, no --unused) → suspend REPL and run the interactive TUI.
			rawParts := strings.Fields(raw)
			if len(rawParts) > 0 && rawParts[0] == "graph" {
				gArgs := rawParts[1:]
				format := flagValue(gArgs, "--format")
				if (format == "" || format == "ascii") &&
					!hasFlag(gArgs, "--unused") &&
					!hasFlag(gArgs, "--no-interactive") {
					loomBin, _ := os.Executable()
					sub := exec.Command(loomBin, append([]string{"graph"}, gArgs...)...)
					m.input.SetValue("")
					return m, tea.ExecProcess(sub, func(err error) tea.Msg {
						if err != nil {
							return cmdResultMsg{
								output: ErrorStyle.Render("graph: "+err.Error()) + "\n",
								isErr:  true,
							}
						}
						return cmdResultMsg{output: "", isErr: false}
					})
				}
			}

			m.running = true
			m.runningCmd = raw
			return m, tea.Batch(m.execCmd(raw), m.spinner.Tick)

		case tea.KeyTab:
			cur := m.input.Value()
			if !m.compActive {
				m.completions = Completions(cur, m.promptNames)
				if len(m.completions) == 0 {
					return m, nil
				}
				m.compActive = true
				m.compIdx = 0
			} else {
				m.compIdx = (m.compIdx + 1) % len(m.completions)
			}
			m.input.SetValue(applyCompletion(cur, m.completions[m.compIdx]))
			m.input.CursorEnd()
			return m, nil

		case tea.KeyEscape:
			m.compActive = false
			m.compIdx = -1
			m.completions = nil
			return m, nil

		case tea.KeyUp:
			if len(m.history) == 0 {
				return m, nil
			}
			if m.histIdx == -1 {
				m.savedInput = m.input.Value()
				m.histIdx = len(m.history) - 1
			} else if m.histIdx > 0 {
				m.histIdx--
			}
			m.input.SetValue(m.history[m.histIdx])
			m.input.CursorEnd()
			m.compActive = false
			return m, nil

		case tea.KeyDown:
			if m.histIdx == -1 {
				return m, nil
			}
			if m.histIdx < len(m.history)-1 {
				m.histIdx++
				m.input.SetValue(m.history[m.histIdx])
			} else {
				m.histIdx = -1
				m.input.SetValue(m.savedInput)
			}
			m.input.CursorEnd()
			m.compActive = false
			return m, nil

		default:
			m.compActive = false
			m.compIdx = -1
			m.completions = nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	// Check if the new input value triggered (or exited) the # picker.
	m = m.syncPicker()
	return m, cmd
}

// syncPicker checks the current input value for an active '#' token and
// updates picker state accordingly.
func (m replModel) syncPicker() replModel {
	val := m.input.Value()
	hashIdx, filter, ok := ExtractHashFilter(val)
	if !ok {
		if m.pickerActive {
			m.pickerActive = false
			m.pickerFiltered = nil
			m.pickerIdx = 0
		}
		return m
	}
	m.pickerActive = true
	m.pickerHashByte = hashIdx
	m.pickerFiltered = FilterPickerItems(m.pickerAll, filter)
	// Reset selection only when filter changes; keep position when navigating.
	if m.pickerIdx >= len(m.pickerFiltered) {
		m.pickerIdx = 0
	}
	return m
}

// applyPickerSelection inserts the selected item's Insert value into the input,
// replacing the '#<filter>' token.
func (m replModel) applyPickerSelection() replModel {
	if len(m.pickerFiltered) == 0 {
		return m
	}
	item := m.pickerFiltered[m.pickerIdx]
	val := m.input.Value()
	before := val[:m.pickerHashByte]
	newVal := before + item.Insert + " "
	m.input.SetValue(newVal)
	m.input.CursorEnd()
	m.pickerActive = false
	m.pickerFiltered = nil
	m.pickerIdx = 0
	return m
}

// closePicker closes the picker, optionally removing the '#' token from input.
func (m replModel) closePicker(removeHash bool) replModel {
	if removeHash {
		val := m.input.Value()
		if m.pickerHashByte < len(val) {
			// Remove everything from # to end (the whole partial token).
			_ = val[:m.pickerHashByte+1]
			newVal := val[:m.pickerHashByte]
			m.input.SetValue(newVal)
			m.input.CursorEnd()
		}
	}
	m.pickerActive = false
	m.pickerFiltered = nil
	m.pickerIdx = 0
	return m
}

func (m replModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Determine how many banner lines to show (reveal animation).
	bannerAllLines := strings.Split(m.bannerString, "\n")
	visibleLines := m.bannerLines
	if visibleLines > len(bannerAllLines) {
		visibleLines = len(bannerAllLines)
	}

	// Glow state: odd glow ticks = bright flash, even = normal.
	glowActive := m.bannerGlowTick > 0 && m.bannerGlowTick%2 == 1
	// Number of logo lines (the ASCII art block at the top).
	const logoLineCount = 6

	// Calculate how many rows the bottom UI occupies.
	const baseRows = 2 // divider + input
	var extraRows int
	if m.pickerActive && len(m.pickerFiltered) >= 0 {
		// picker box: top border + filter + separator + items (max 8) + bottom border
		visible := len(m.pickerFiltered)
		if visible > 8 {
			visible = 8
		}
		if visible == 0 {
			visible = 1 // "no matches" line
		}
		extraRows = visible + 4
	} else if m.compActive && len(m.completions) > 0 {
		extraRows = 1
	} else {
		extraRows = 1
	}

	outputHeight := m.height - visibleLines - baseRows - extraRows
	if outputHeight < 1 {
		outputHeight = 1
	}

	var b strings.Builder

	// Banner (partial reveal during animation).
	// The last visible line is the "scanline" — rendered in Accent color for a
	// neon sweep effect. After full reveal, logo lines pulse bright on glow ticks.
	scanlineIdx := visibleLines - 1
	glowStyle := lipgloss.NewStyle().Foreground(clrBright).Bold(true)
	scanStyle := lipgloss.NewStyle().Foreground(clrAccent).Bold(true)
	for i := 0; i < visibleLines; i++ {
		line := bannerAllLines[i]
		isLogoLine := i < logoLineCount
		switch {
		case i == scanlineIdx && m.bannerLines < m.bannerTotalLines:
			// Active scanline during reveal — accent flash.
			b.WriteString(scanStyle.Render(line))
		case isLogoLine && glowActive:
			// Settle-pulse glow — brief bright flash.
			b.WriteString(glowStyle.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteByte('\n')
	}

	// Output area.
	start := 0
	if len(m.outputLines) > outputHeight {
		start = len(m.outputLines) - outputHeight
	}
	for i := start; i < len(m.outputLines); i++ {
		b.WriteString(m.outputLines[i] + "\n")
	}
	for i := len(m.outputLines) - start; i < outputHeight; i++ {
		b.WriteByte('\n')
	}

	// # Picker panel (replaces completions row when active).
	if m.pickerActive {
		b.WriteString(RenderPickerPanel(m.pickerFiltered, m.pickerIdx,
			m.pickerFilter(), m.width))
	} else if m.compActive && len(m.completions) > 0 {
		parts := make([]string, len(m.completions))
		for i, c := range m.completions {
			if i == m.compIdx {
				parts[i] = SelectedCompStyle.Render(c)
			} else {
				parts[i] = UnselectedCompStyle.Render(c)
			}
		}
		b.WriteString("  " + strings.Join(parts, " ") + "\n")
	} else {
		b.WriteByte('\n')
	}

	// Divider.
	dw := m.width - 4
	if dw < 1 {
		dw = 1
	}
	b.WriteString(lipgloss.NewStyle().Foreground(clrDim).Render(
		"  "+strings.Repeat("─", dw)) + "\n")

	// Input line — show spinner when a command is running.
	if m.running {
		sp := m.spinner
		sp.Style = lipgloss.NewStyle().Foreground(clrPrimary)
		b.WriteString("  " + sp.View() + "  " + MutedStyle.Render(m.runningCmd) + "\n")
	} else {
		b.WriteString("  " + m.input.View())
	}

	return b.String()
}

// pickerFilter extracts the current filter text (text after '#').
func (m replModel) pickerFilter() string {
	_, filter, _ := ExtractHashFilter(m.input.Value())
	return filter
}

// execCmd dispatches user input as a background tea.Cmd.
func (m replModel) execCmd(raw string) tea.Cmd {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil
	}
	verb := parts[0]
	args := parts[1:]
	cwd := m.cwd

	return func() tea.Msg {
		out, isErr := dispatch(verb, args, cwd)
		return cmdResultMsg{output: out, isErr: isErr}
	}
}

func dispatch(verb string, args []string, cwd string) (string, bool) {
	switch verb {

	case "theme":
		if len(args) == 0 {
			return fmt.Sprintf("  %s  current theme: %s\n",
				MutedStyle.Render("◈"), PromptNameStyle.Render(CurrentThemeName())), false
		}
		name := args[0]
		if !SetTheme(name) {
			return ErrorStyle.Render(fmt.Sprintf("Error: unknown theme %q — use 'light' or 'dark'", name)) + "\n", true
		}
		icon := "◐"
		if name == "dark" {
			icon = "◑"
		}
		return fmt.Sprintf("  %s  theme: %s\n",
			SuccessStyle.Render(icon), PromptNameStyle.Render(name)), false

	case "inspect":
		out, hasErr := RunInspect(cwd)
		return out, hasErr

	case "list":
		onlyP := hasFlag(args, "--prompts")
		onlyB := hasFlag(args, "--blocks")
		return RunList(cwd, onlyP, onlyB), false

	case "trace":
		name := firstNonFlagArg(args)
		traceOpts := TraceOptions{
			Field:       flagValue(args, "--field"),
			Instruction: flagValue(args, "--instruction"),
			Tree:        hasFlag(args, "--tree"),
		}
		if name == "" && !traceOpts.Tree {
			return ErrorStyle.Render("Usage: trace <PromptName>  or  trace <folder/>") + "\n", true
		}
		if strings.HasSuffix(name, "/") {
			out, err := RunTraceFolder(name[:len(name)-1], cwd)
			if err != nil {
				return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
			}
			return out, false
		}
		out, err := RunTrace(name, traceOpts, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "unravel":
		if len(args) == 0 {
			return ErrorStyle.Render("Usage: unravel <PromptName>  or  unravel <folder/>") + "\n", true
		}
		name := args[0]
		withSrc := hasFlag(args, "--with-source")
		if strings.HasSuffix(name, "/") {
			out, err := RunUnravelFolder(name[:len(name)-1], withSrc, cwd)
			if err != nil {
				return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
			}
			return out, false
		}
		out, err := RunUnravel(name, withSrc, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "weave":
		all := hasFlag(args, "--all")
		watch := hasFlag(args, "--watch")
		incr := hasFlag(args, "--incr") || hasFlag(args, "--incremental")
		if watch {
			return WarningStyle.Render("Watch mode is not supported inside the REPL.") +
				"\n  Exit with Ctrl+C and run: " + CommandStyle.Render("loom weave --all --watch") + "\n", true
		}
		stdout := hasFlag(args, "--stdout")
		outPath := flagValue(args, "--out")
		format := flagValue(args, "--format")
		variant := flagValue(args, "--variant")
		profile := flagValue(args, "--profile")
		overlays := flagValues(args, "--overlay")
		sourceMap := hasFlag(args, "--sourcemap")
		sets := append(flagValues(args, "--set"), flagValues(args, "--slot")...)
		varsFile := flagValue(args, "--vars")
		values := map[string]string{}
		if varsFile != "" {
			path := varsFile
			if !filepath.IsAbs(path) {
				path = filepath.Join(cwd, path)
			}
			fileVars, err := LoadVarsFile(path)
			if err != nil {
				return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
			}
			values = fileVars
		}
		flagVars, err := ParseKVArgs(sets)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		for key, value := range flagVars {
			values[key] = value
		}
		name := firstNonFlagArg(args)
		if !all && name == "" {
			return ErrorStyle.Render("Usage: weave <PromptName>  or  weave --all  or  weave <folder/>") + "\n", true
		}
		// Folder batch weave.
		if strings.HasSuffix(name, "/") {
			out, err := RunWeaveFolder(name[:len(name)-1], WeaveOptions{
				Format:    format,
				Variables: values,
				Profile:   profile,
				Variant:   variant,
				Overlays:  overlays,
			}, cwd)
			if err != nil {
				return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
			}
			return out, false
		}
		out, err := RunWeave(name, all, WeaveOptions{
			OutPath:          outPath,
			Stdout:           stdout,
			Format:           format,
			Variables:        values,
			Profile:          profile,
			Variant:          variant,
			Overlays:         overlays,
			SourceMap:        sourceMap,
			Incremental:      incr,
			InteractiveSlots: false,
		}, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "deploy":
		out, err := RunDeploy(DeployOptions{
			DryRun:       hasFlag(args, "--dry-run"),
			Diff:         hasFlag(args, "--diff"),
			TargetFormat: flagValue(args, "--target"),
		}, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "fmt":
		check := hasFlag(args, "--check")
		out, err := RunFmt(check, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "fingerprint":
		name := firstNonFlagArg(args)
		if name == "" {
			return ErrorStyle.Render("Usage: fingerprint <PromptName>") + "\n", true
		}
		out, err := RunFingerprint(name, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "thread":
		if len(args) < 2 {
			return ErrorStyle.Render("Usage: thread prompt <Name>  or  thread block <Name>") + "\n", true
		}
		kind, name := args[0], args[1]
		out, err := runThread(kind, name, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "doctor":
		name := firstNonFlagArg(args)
		all := name == ""
		out, err := RunDoctor(name, all, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "smells":
		name := firstNonFlagArg(args)
		all := name == ""
		out, err := RunSmells(name, all, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "contract":
		name := firstNonFlagArg(args)
		if name == "" {
			return ErrorStyle.Render("Usage: contract <PromptName>") + "\n", true
		}
		out, err := RunContract(name, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "check-output":
		if len(args) < 2 {
			return ErrorStyle.Render("Usage: check-output <PromptName> <output-file>") + "\n", true
		}
		promptName := args[0]
		outputPath := args[1]
		out, passed, err := RunCheckOutput(promptName, outputPath, cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, !passed

	case "lock":
		out, err := RunLock(cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, false

	case "check-lock":
		out, mismatch, err := RunCheckLock(cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, mismatch

	case "ci":
		out, failed, err := RunCI(cwd)
		if err != nil {
			return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
		}
		return out, failed

	case "graph":
		name := firstNonFlagArg(args)
		format := flagValue(args, "--format")
		if format == "" {
			format = "ascii"
		}
		unused := hasFlag(args, "--unused")
		out, isErr := RunGraph(name, format, unused, cwd)
		return out, isErr

	case "stats":
		all := hasFlag(args, "--all")
		limit := 0
		if lv := flagValue(args, "--limit"); lv != "" {
			fmt.Sscanf(lv, "%d", &limit)
		}
		name := firstNonFlagArg(args)
		if !all && name == "" {
			return ErrorStyle.Render("Usage: stats <PromptName>  or  stats --all") + "\n", true
		}
		out, isErr := RunStats(name, all, limit, cwd)
		return out, isErr

	case "pack":
		if len(args) == 0 {
			return ErrorStyle.Render("Usage: pack <init|build|install|list|remove>") + "\n", true
		}
		switch args[0] {
		case "init":
			return RunPackInit(cwd)
		case "build":
			return RunPackBuild(cwd)
		case "install":
			if len(args) < 2 {
				return ErrorStyle.Render("Usage: pack install <path>") + "\n", true
			}
			return RunPackInstall(args[1], cwd)
		case "list":
			return RunPackList(cwd), false
		case "remove":
			if len(args) < 2 {
				return ErrorStyle.Render("Usage: pack remove <name>") + "\n", true
			}
			return RunPackRemove(args[1], cwd)
		default:
			return ErrorStyle.Render(fmt.Sprintf("Unknown pack subcommand %q", args[0])) + "\n", true
		}

	case "help":
		return renderHelp(), false

	default:
		return ErrorStyle.Render(fmt.Sprintf("Unknown command %q — type 'help'.", verb)) + "\n", true
	}
}

func runThread(kind, name, cwd string) (string, error) {
	cfg, err := config.Load(cwd)
	if err != nil {
		return "", err
	}
	var dir, header string
	var ext string
	switch kind {
	case "prompt":
		dir = filepath.Join(cwd, cfg.Paths.Prompts)
		header = "prompt"
		ext = ".prompt.loom"
	case "block":
		dir = filepath.Join(cwd, cfg.Paths.Blocks)
		header = "block"
		ext = ".block.loom"
	case "overlay":
		dir = filepath.Join(cwd, cfg.Paths.Overlays)
		header = "overlay"
		ext = ".overlay.loom"
	default:
		return "", fmt.Errorf("unknown kind %q — use 'prompt', 'block', or 'overlay'", kind)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	filename := toKebab(name) + ext
	path := filepath.Join(dir, filename)
	if _, err := os.Stat(path); err == nil {
		return WarningStyle.Render(fmt.Sprintf("  %s already exists — skipping.", filename)) + "\n", nil
	}
	content := fmt.Sprintf(
		"%s %s {\n  summary :=\n    Describe this %s.\n\n  objective :=\n    What should this %s do?\n}\n",
		header, name, kind, kind)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return "  " + SuccessStyle.Render("✓") + "  " + PathStyle.Render(path) + "\n", nil
}

func renderHelp() string {
	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Available Commands") + "\n")
	b.WriteString("  " + Divider(50) + "\n")
	rows := []struct{ cmd, desc string }{
		{"weave <Name>          ", "Render a prompt artifact"},
		{"weave --format plain  ", "Choose markdown/json/tool output"},
		{"weave --sourcemap     ", "Write a .loom.map.json sidecar"},
		{"weave --profile local ", "Apply a named variable profile"},
		{"weave --set k=v       ", "Override a render variable or slot"},
		{"weave --variant deep  ", "Apply a named variant"},
		{"weave --overlay name  ", "Apply an overlay"},
		{"weave <folder/>       ", "Render all prompts in a subfolder"},
		{"weave --all           ", "Render all prompts"},
		{"deploy                ", "Write configured deploy targets"},
		{"deploy --dry-run      ", "Preview deploy writes"},
		{"deploy --diff         ", "Show target content diffs"},
		{"deploy --target x     ", "Deploy one target format only"},
		{"inspect               ", "Validate the prompt library"},
		{"list                  ", "List all prompts and blocks"},
		{"list --prompts        ", "List only prompts"},
		{"list --blocks         ", "List only blocks"},
		{"trace <Name>          ", "Show inheritance chain + field sources"},
		{"trace --field x       ", "Trace one resolved field"},
		{"trace --instruction x ", "Find a resolved instruction source"},
		{"trace --tree          ", "Render the full project tree"},
		{"trace <folder/>       ", "Trace all prompts in a subfolder"},
		{"fingerprint <Name>    ", "Print the stable prompt fingerprint"},
		{"unravel <Name>        ", "Show fully resolved fields"},
		{"unravel --with-source ", "Show resolved fields with sources"},
		{"doctor                ", "Check all prompt health + smells"},
		{"doctor <Name>         ", "Check one prompt health + smells"},
		{"smells                ", "List all smell warnings in the library"},
		{"smells <Name>         ", "List smells for one prompt"},
		{"contract <Name>       ", "Print contract and capabilities"},
		{"check-output <N> <f>  ", "Validate output file against contract"},
		{"lock                  ", "Generate / update loom.lock"},
		{"check-lock            ", "Verify prompts match loom.lock"},
		{"ci                    ", "Run all CI gates"},
		{"thread prompt <Name>  ", "Scaffold a new .prompt.loom file"},
		{"thread block <Name>   ", "Scaffold a new .block.loom file"},
		{"thread overlay <Name> ", "Scaffold a new .overlay.loom file"},
		{"thread vars <Name>    ", "Scaffold a new .vars.loom file"},
		{"fmt                   ", "Format .loom source files"},
		{"fmt --check           ", "Check formatting (no writes)"},
		{"theme [dark|light]    ", "Switch color theme"},
		{"clear                 ", "Clear output area"},
		{"exit                  ", "Quit the REPL"},
	}
	for _, r := range rows {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			CommandStyle.Render(r.cmd), ArgDescStyle.Render(r.desc)))
	}
	b.WriteString("\n  " + MutedStyle.Render("Tip: type # anywhere to open the prompt picker.") + "\n\n")
	return b.String()
}

func applyCompletion(input, choice string) string {
	parts := strings.Fields(input)
	trailing := len(input) > 0 && input[len(input)-1] == ' '
	if len(parts) == 0 || trailing {
		if len(parts) == 0 {
			return choice + " "
		}
		return strings.Join(parts, " ") + " " + choice + " "
	}
	parts[len(parts)-1] = choice
	return strings.Join(parts, " ") + " "
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, flag+"=") {
			return strings.TrimPrefix(a, flag+"=")
		}
	}
	return ""
}

func flagValues(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
			continue
		}
		if strings.HasPrefix(a, flag+"=") {
			out = append(out, strings.TrimPrefix(a, flag+"="))
		}
	}
	return out
}

func firstNonFlagArg(args []string) string {
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if !strings.Contains(arg, "=") && i+1 < len(args) {
				switch arg {
				case "--out", "--profile", "--variant", "--overlay", "--set", "--slot", "--vars", "--field", "--instruction", "--format", "--target":
					skipNext = true
				}
			}
			continue
		}
		return arg
	}
	return ""
}

func toKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
