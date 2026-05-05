package tui

// graphview.go — interactive split-panel dependency graph browser.
// Left panel: collapsible prompt tree.  Right panel: detail card for selected node.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sayandeepgiri/promptloom/internal/ast"
	igraph "github.com/sayandeepgiri/promptloom/internal/graph"
	"github.com/sayandeepgiri/promptloom/internal/loader"
)

// gvItem is one row in the interactive tree panel.
type gvItem struct {
	name        string
	linePrefix  string // tree-drawing characters before the node name, e.g. "│   └── "
	hasChildren bool
	isExpanded  bool
}

type graphViewModel struct {
	g         *igraph.Graph
	nodeMap   map[string]*ast.Node // name → AST node
	items     []gvItem
	collapsed map[string]bool // name → true when subtree is hidden
	cursor    int
	treeOff   int // first visible row in the tree panel
	detailOff int // first visible line in the detail panel
	width     int
	height    int
	loadErr   string
}

// RunGraphInteractive launches the full-screen interactive graph browser.
// initialName pre-positions the cursor at that node when non-empty.
func RunGraphInteractive(cwd, initialName string) error {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return err
	}

	g := igraph.Build(reg)
	nodeMap := make(map[string]*ast.Node)
	for _, n := range reg.Prompts() {
		nodeMap[n.Name] = n
	}

	m := &graphViewModel{
		g:         g,
		nodeMap:   nodeMap,
		collapsed: make(map[string]bool),
	}
	m.buildItems()

	// Pre-position cursor at the requested node.
	if initialName != "" {
		for i, it := range m.items {
			if it.name == initialName {
				m.cursor = i
				break
			}
		}
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, runErr := p.Run()
	return runErr
}

// addItem appends name and its visible subtree to m.items.
// linePrefix  — characters rendered before the node name ("│   └── " etc).
// childIndent — shared indent passed down so children can build their own prefix.
func (m *graphViewModel) addItem(name, linePrefix, childIndent string) {
	children := m.g.Children(name)
	hasChildren := len(children) > 0
	expanded := !m.collapsed[name]

	m.items = append(m.items, gvItem{
		name:        name,
		linePrefix:  linePrefix,
		hasChildren: hasChildren,
		isExpanded:  expanded,
	})

	if !hasChildren || !expanded {
		return
	}

	for i, child := range children {
		isLast := i == len(children)-1
		var cPrefix, cIndent string
		if isLast {
			cPrefix = childIndent + "└── "
			cIndent = childIndent + "    "
		} else {
			cPrefix = childIndent + "├── "
			cIndent = childIndent + "│   "
		}
		m.addItem(child, cPrefix, cIndent)
	}
}

func (m *graphViewModel) buildItems() {
	m.items = nil
	for _, root := range m.g.Roots() {
		m.addItem(root, "", "")
	}
}

// Init satisfies tea.Model.
func (m *graphViewModel) Init() tea.Cmd { return nil }

// Update satisfies tea.Model.
func (m *graphViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	availH := m.height - 2 // 1 title + 1 footer
	if availH < 1 {
		availH = 1
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.detailOff = 0
				m.keepCursorVisible(availH)
			}

		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
				m.detailOff = 0
				m.keepCursorVisible(availH)
			}

		case "g", "home":
			m.cursor = 0
			m.treeOff = 0
			m.detailOff = 0

		case "G", "end":
			m.cursor = len(m.items) - 1
			m.detailOff = 0
			m.keepCursorVisible(availH)

		case " ":
			if m.cursor < len(m.items) {
				item := m.items[m.cursor]
				if item.hasChildren {
					// Toggle collapse, keeping cursor on the same node after rebuild.
					cursorName := item.name
					if m.collapsed[item.name] {
						delete(m.collapsed, item.name)
					} else {
						m.collapsed[item.name] = true
					}
					m.buildItems()
					for i, it := range m.items {
						if it.name == cursorName {
							m.cursor = i
							break
						}
					}
					m.keepCursorVisible(availH)
				}
			}

		case "e":
			// Expand all nodes.
			m.collapsed = make(map[string]bool)
			m.buildItems()
			m.keepCursorVisible(availH)

		case "c":
			// Collapse all nodes that have children.
			for _, it := range m.items {
				if it.hasChildren {
					m.collapsed[it.name] = true
				}
			}
			m.buildItems()
			m.cursor = 0
			m.treeOff = 0

		case "pgup", "ctrl+b":
			m.detailOff -= availH / 2
			if m.detailOff < 0 {
				m.detailOff = 0
			}

		case "pgdn", "ctrl+f":
			m.detailOff += availH / 2
		}
	}

	return m, nil
}

func (m *graphViewModel) keepCursorVisible(visH int) {
	if m.cursor < m.treeOff {
		m.treeOff = m.cursor
	}
	if m.cursor >= m.treeOff+visH {
		m.treeOff = m.cursor - visH + 1
	}
	if m.treeOff < 0 {
		m.treeOff = 0
	}
}

// View satisfies tea.Model.
func (m *graphViewModel) View() string {
	if m.loadErr != "" {
		return ErrorStyle.Render("  error: "+m.loadErr) + "\n"
	}
	if m.width == 0 {
		return "Loading…"
	}
	if len(m.items) == 0 {
		return MutedStyle.Render("  No prompts found in library.\n")
	}

	availH := m.height - 2
	if availH < 1 {
		availH = 1
	}

	// Split width: tree gets 40 % clamped to [22, 50].
	leftW := m.width * 2 / 5
	if leftW < 22 {
		leftW = 22
	}
	if leftW > 50 {
		leftW = 50
	}
	rightW := m.width - leftW - 3 // 3 = " │ "
	if rightW < 10 {
		rightW = 10
	}

	treeLines := m.renderTree(leftW, availH)
	detailLines := m.renderDetail(rightW, availH)

	colDiv := MutedStyle.Render("│")
	var rows []string
	for i := 0; i < availH; i++ {
		var left, right string
		if i < len(treeLines) {
			left = treeLines[i]
		}
		if i < len(detailLines) {
			right = detailLines[i]
		}
		rows = append(rows, padToWidth(left, leftW)+" "+colDiv+" "+right)
	}

	title := HeaderStyle.Render(" Dependency Graph ") +
		MutedStyle.Render("↑↓/jk navigate · Space expand · e expand-all · c collapse-all · PgUp/PgDn scroll detail · q quit")
	footer := MutedStyle.Render(fmt.Sprintf(" %d prompts", len(m.items)))
	return title + "\n" + strings.Join(rows, "\n") + "\n" + footer
}

// renderTree builds the visible slice of tree rows (already scrolled).
func (m *graphViewModel) renderTree(width, height int) []string {
	all := make([]string, 0, len(m.items))

	for i, item := range m.items {
		isCursor := i == m.cursor

		indicator := "  "
		if item.hasChildren {
			if item.isExpanded {
				indicator = "▼ "
			} else {
				indicator = "▶ "
			}
		}

		prefix := MutedStyle.Render(item.linePrefix)

		var indStr, nameStr string
		if isCursor {
			indStr = BrightStyle.Render(indicator)
			nameStr = FocusedPromptStyle.Render(" " + item.name + " ")
		} else {
			indStr = MutedStyle.Render(indicator)
			nameStr = PromptNameStyle.Render(item.name)
		}

		// Block-count badge.
		badge := ""
		if node, ok := m.nodeMap[item.name]; ok && len(node.Uses) > 0 {
			badge = " " + MutedStyle.Render(fmt.Sprintf("[%d blk]", len(node.Uses)))
		}

		all = append(all, prefix+indStr+nameStr+badge)
	}

	// Scroll window.
	if m.treeOff >= len(all) {
		m.treeOff = 0
	}
	end := m.treeOff + height
	if end > len(all) {
		end = len(all)
	}
	visible := make([]string, height)
	copy(visible, all[m.treeOff:end])
	return visible
}

// renderDetail builds the visible slice of detail-panel lines (already scrolled).
func (m *graphViewModel) renderDetail(width, height int) []string {
	if len(m.items) == 0 || m.cursor >= len(m.items) {
		return blanks(height)
	}

	name := m.items[m.cursor].name
	node := m.nodeMap[name]
	if node == nil {
		return blanks(height)
	}

	hr := DividerStyle.Render(strings.Repeat("─", width-1))
	shortHR := MutedStyle.Render(strings.Repeat("─", 24))

	var all []string

	// ── Header ──
	all = append(all, PromptNameStyle.Render(name))
	all = append(all, hr)
	all = append(all, "")

	// ── Metadata grid ──
	kindStr := "prompt"
	if node.Kind == ast.KindBlock {
		kindStr = "block"
	}
	all = append(all, detailRow("Kind", MutedStyle.Render(kindStr)))

	if node.Parent != "" {
		all = append(all, detailRow("Parent", InheritsStyle.Render("▸ "+node.Parent)))
	} else {
		all = append(all, detailRow("Parent", MutedStyle.Render("—")))
	}

	children := m.g.Children(name)
	if len(children) > 0 {
		childStr := strings.Join(children, ", ")
		if lipgloss.Width(childStr) > width-14 {
			childStr = fmt.Sprintf("%d prompts", len(children))
		}
		all = append(all, detailRow("Children", PromptNameStyle.Render(childStr)))
	}

	if len(node.Uses) > 0 {
		parts := make([]string, len(node.Uses))
		for i, b := range node.Uses {
			parts[i] = BlockNameStyle.Render(b)
		}
		all = append(all, detailRow("Blocks", strings.Join(parts, MutedStyle.Render(", "))))
	} else {
		all = append(all, detailRow("Blocks", MutedStyle.Render("—")))
	}

	if len(node.Variants) > 0 {
		vparts := make([]string, len(node.Variants))
		for i, v := range node.Variants {
			vparts[i] = BrightStyle.Render(v.Name)
		}
		all = append(all, detailRow("Variants", strings.Join(vparts, MutedStyle.Render(", "))))
	}

	// ── Variables / Slots ──
	if len(node.Vars) > 0 {
		all = append(all, "")
		all = append(all, SubHeaderStyle.Render(" Variables"))
		all = append(all, shortHR)
		for _, v := range node.Vars {
			var valStr string
			if v.IsSlot {
				valStr = WarningStyle.Render("‹slot›")
				if v.Required {
					valStr += "  " + ErrorStyle.Render("required")
				}
			} else {
				if v.Default != "" {
					valStr = TextStyle.Render(`"` + v.Default + `"`)
				} else {
					valStr = MutedStyle.Render(`""`)
				}
			}
			all = append(all, fmt.Sprintf("  %s  =  %s",
				BlockNameStyle.Render(v.Name), valStr))
		}
	}

	// ── Fields ──
	if len(node.Fields) > 0 {
		all = append(all, "")
		all = append(all, SubHeaderStyle.Render(" Fields"))
		all = append(all, shortHR)
		for _, f := range node.Fields {
			opStr := MutedStyle.Render(f.Op.String())
			all = append(all, "  "+BrightStyle.Render(f.FieldName)+opStr)
			for _, val := range f.Value {
				for _, wl := range softWrap(val, width-6) {
					all = append(all, "    "+TextStyle.Render(wl))
				}
			}
		}
	}

	// ── Contract ──
	if c := node.Contract; c != nil {
		all = append(all, "")
		all = append(all, SubHeaderStyle.Render(" Contract"))
		all = append(all, shortHR)
		if len(c.RequiredSections) > 0 {
			all = append(all, detailRow("require", TextStyle.Render(strings.Join(c.RequiredSections, ", "))))
		}
		if len(c.ForbiddenSections) > 0 {
			all = append(all, detailRow("forbid", TextStyle.Render(strings.Join(c.ForbiddenSections, ", "))))
		}
		if len(c.MustInclude) > 0 {
			all = append(all, detailRow("must have", TextStyle.Render(strings.Join(c.MustInclude, ", "))))
		}
		if len(c.MustNotInclude) > 0 {
			all = append(all, detailRow("must not", TextStyle.Render(strings.Join(c.MustNotInclude, ", "))))
		}
	}

	// ── Capabilities ──
	if cap := node.Capabilities; cap != nil {
		all = append(all, "")
		all = append(all, SubHeaderStyle.Render(" Capabilities"))
		all = append(all, shortHR)
		if len(cap.Allowed) > 0 {
			all = append(all, detailRow("allowed", SuccessStyle.Render(strings.Join(cap.Allowed, ", "))))
		}
		if len(cap.Forbidden) > 0 {
			all = append(all, detailRow("forbidden", ErrorStyle.Render(strings.Join(cap.Forbidden, ", "))))
		}
	}

	all = append(all, "")

	// Clamp detailOff.
	maxOff := len(all) - height
	if maxOff < 0 {
		maxOff = 0
	}
	if m.detailOff > maxOff {
		m.detailOff = maxOff
	}

	start := m.detailOff
	end := start + height
	if end > len(all) {
		end = len(all)
	}
	visible := all[start:end]

	// Pad to height.
	result := make([]string, height)
	copy(result, visible)
	return result
}

// detailRow formats a two-column metadata row.
func detailRow(label, value string) string {
	return MutedStyle.Render(fmt.Sprintf("  %-12s", label)) + value
}

// softWrap wraps a string at word boundaries to at most width characters per line.
func softWrap(s string, width int) []string {
	if width <= 4 || len(s) <= width {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		at := width
		for at > 0 && s[at-1] != ' ' {
			at--
		}
		if at == 0 {
			at = width
		}
		lines = append(lines, s[:at])
		s = strings.TrimLeft(s[at:], " ")
	}
	if s != "" {
		lines = append(lines, s)
	}
	return lines
}

// padToWidth pads an ANSI-styled string to exactly width visual characters.
func padToWidth(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

// blanks returns a slice of height empty strings.
func blanks(height int) []string {
	out := make([]string, height)
	return out
}
