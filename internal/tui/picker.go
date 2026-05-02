package tui

// picker.go — # prompt/folder picker for the REPL.
//
// Typing '#' anywhere in the command line opens a picker panel showing all
// prompt names and any subdirectories of the prompts/ folder.  Selecting a
// folder item (ending with '/') causes the action to run against every prompt
// file inside that folder.

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/loader"
)

// PickerItem is one selectable entry in the # picker.
type PickerItem struct {
	Display  string // shown in the panel
	Insert   string // replaces the '#...' token in the input
	IsFolder bool
	Meta     string // e.g. "inherits BaseAssistant" or "3 prompts"
}

// BuildPickerItems builds the full, unfiltered picker list for cwd.
// Order: "all prompts" special item → subfolders (sorted) → individual prompts (sorted).
func BuildPickerItems(cwd string) []PickerItem {
	cfg, err := config.Load(cwd)
	if err != nil {
		return nil
	}
	promptDir := filepath.Join(cwd, cfg.Paths.Prompts)

	// Walk the directory tree once to collect top-level files and subfolders.
	folderCounts := map[string]int{}
	topLevelCount := 0

	_ = filepath.WalkDir(promptDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".prompt") {
			return nil
		}
		rel, _ := filepath.Rel(promptDir, path)
		dir := filepath.Dir(rel)
		if dir == "." {
			topLevelCount++
		} else {
			folderCounts[dir]++
		}
		return nil
	})

	totalPrompts := topLevelCount
	for _, c := range folderCounts {
		totalPrompts += c
	}

	var items []PickerItem

	// "all prompts" — maps to --all flag.
	if totalPrompts > 1 {
		items = append(items, PickerItem{
			Display:  "all prompts",
			Insert:   "--all",
			IsFolder: true,
			Meta:     fmt.Sprintf("%d prompts", totalPrompts),
		})
	}

	// Subdirectory items — each maps to "folder/" convention.
	var folders []string
	for f := range folderCounts {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	for _, f := range folders {
		items = append(items, PickerItem{
			Display:  f + "/",
			Insert:   f + "/",
			IsFolder: true,
			Meta:     fmt.Sprintf("%d prompts", folderCounts[f]),
		})
	}

	// Individual prompts from the registry (includes parent info).
	reg, _, _ := loader.Load(cwd)
	if reg != nil {
		prompts := reg.Prompts()
		sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
		for _, p := range prompts {
			meta := ""
			if p.Parent != "" {
				meta = "inherits " + p.Parent
			}
			items = append(items, PickerItem{
				Display:  p.Name,
				Insert:   p.Name,
				IsFolder: false,
				Meta:     meta,
			})
		}
	}

	return items
}

// FilterPickerItems returns items whose Display contains filter (case-insensitive).
func FilterPickerItems(items []PickerItem, filter string) []PickerItem {
	if filter == "" {
		return items
	}
	lower := strings.ToLower(filter)
	var out []PickerItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.Display), lower) {
			out = append(out, it)
		}
	}
	return out
}

// ExtractHashFilter scans input for an active '#' token immediately before
// the end of the string (no space between '#' and end).
// Returns (byteIndex of '#', filter text after '#', ok).
func ExtractHashFilter(s string) (int, string, bool) {
	last := strings.LastIndex(s, "#")
	if last < 0 {
		return 0, "", false
	}
	after := s[last+1:]
	if strings.Contains(after, " ") {
		return 0, "", false
	}
	return last, after, true
}

// RenderPickerPanel renders the picker box as a multi-line styled string.
// selIdx is the index within filtered (not full) items.
func RenderPickerPanel(filtered []PickerItem, selIdx int, filter string, termWidth int) string {
	const maxVisible = 8
	const minWidth = 44

	boxWidth := termWidth - 6 // 2 margin + 2 border chars + 2 padding
	if boxWidth < minWidth {
		boxWidth = minWidth
	}
	if boxWidth > 72 {
		boxWidth = 72
	}

	var b strings.Builder
	border := DividerStyle.Render

	// ── Top border with title ──────────────────────────────────────────────
	title := SubHeaderStyle.Render(" # Prompt Picker ")
	titleLen := len(" # Prompt Picker ")
	rightDashes := boxWidth - titleLen - 1
	if rightDashes < 0 {
		rightDashes = 0
	}
	b.WriteString("  " + border("╭─") + title + border(strings.Repeat("─", rightDashes)+"╮") + "\n")

	// ── Filter line ────────────────────────────────────────────────────────
	var filterDisplay string
	if filter == "" {
		filterDisplay = MutedStyle.Render("type to filter…")
	} else {
		filterDisplay = BrightStyle.Render(filter)
	}
	filterLine := MutedStyle.Render("  filter: ") + filterDisplay
	b.WriteString("  " + border("│") + " " + filterLine + "\n")
	b.WriteString("  " + border("│") + " " + border(strings.Repeat("─", boxWidth-1)) + "\n")

	// ── Items ──────────────────────────────────────────────────────────────
	if len(filtered) == 0 {
		b.WriteString("  " + border("│") + " " + MutedStyle.Render("  no matches") + "\n")
	} else {
		// Scroll window so the selected item is always visible.
		start := 0
		if selIdx >= maxVisible {
			start = selIdx - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		for i := start; i < end; i++ {
			it := filtered[i]
			selected := i == selIdx

			// Selector arrow.
			var arrow string
			if selected {
				arrow = SuccessStyle.Render("▶")
			} else {
				arrow = "  "
			}

			// Type badge.
			var badge string
			if it.IsFolder {
				badge = BlockNameStyle.Render("[dir]")
			} else {
				badge = BulletStyle.Render("  ●  ")
			}

			// Name — truncate if needed.
			metaLen := len(it.Meta)
			nameMax := boxWidth - 15 - metaLen // 15 ≈ arrow+badge+spaces+border
			if nameMax < 8 {
				nameMax = 8
			}
			name := it.Display
			if len(name) > nameMax {
				name = name[:nameMax-1] + "…"
			}

			// Padding between name and meta.
			pad := boxWidth - 13 - len(name) - metaLen
			if pad < 1 {
				pad = 1
			}
			paddingStr := strings.Repeat(" ", pad)

			var nameRendered, metaRendered string
			if selected {
				nameRendered = PromptNameStyle.Render(name)
			} else {
				if it.IsFolder {
					nameRendered = BlockNameStyle.Render(name)
				} else {
					nameRendered = TextStyle.Render(name)
				}
			}
			metaRendered = MutedStyle.Render(it.Meta)

			b.WriteString("  " + border("│") + " " +
				arrow + " " + badge + " " +
				nameRendered + paddingStr + metaRendered + "\n")
		}

		// Scroll hint when there are more items below.
		if len(filtered) > maxVisible {
			remaining := len(filtered) - (start + (end - start))
			if remaining > 0 {
				hint := MutedStyle.Render(fmt.Sprintf("  … %d more", remaining))
				b.WriteString("  " + border("│") + " " + hint + "\n")
			}
		}
	}

	// ── Bottom border ──────────────────────────────────────────────────────
	b.WriteString("  " + border("╰"+strings.Repeat("─", boxWidth+1)+"╯") + "\n")

	return b.String()
}
