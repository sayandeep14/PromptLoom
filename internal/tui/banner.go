package tui

import (
	"fmt"
	"path/filepath"
	"strings"
)

const logoLines = `  ██╗      ██████╗  ██████╗ ███╗   ███╗
  ██║     ██╔═══██╗██╔═══██╗████╗ ████║
  ██║     ██║   ██║██║   ██║██╔████╔██║
  ██║     ██║   ██║██║   ██║██║╚██╔╝██║
  ███████╗╚██████╔╝╚██████╔╝██║ ╚═╝ ██║
  ╚══════╝ ╚═════╝  ╚═════╝ ╚═╝     ╚═╝`

// commandHelp is displayed in two columns to keep the banner height manageable.
// Left column is indices 0..N/2-1; right column is N/2..N-1.
var commandHelp = []struct{ cmd, desc string }{
	// --- column 1: authoring & rendering ---
	{"weave <Name>         ", "Render one prompt"},
	{"weave --all          ", "Render all prompts"},
	{"weave --all --watch  ", "Re-render on every file change"},
	{"weave --all --incr   ", "Skip prompts whose hash is unchanged"},
	{"weave --format x     ", "Choose output format"},
	{"weave --sourcemap    ", "Write a source-map sidecar"},
	{"weave --profile x    ", "Apply a named profile"},
	{"weave --set k=v      ", "Override a variable or slot"},
	{"weave --variant x    ", "Apply a named variant"},
	{"weave --overlay x    ", "Apply an overlay"},
	{"weave --env x        ", "Apply environment-specific constraints"},
	{"deploy               ", "Write configured deploy targets"},
	{"deploy --dry-run     ", "Preview target writes"},
	{"thread prompt <Name> ", "Scaffold a new prompt file"},
	{"fmt                  ", "Format prompt files"},
	{"import <file.md>     ", "Convert Markdown prompt to .loom"},
	{"list                 ", "List prompts, blocks, overlays"},
	{"trace <Name>         ", "Show inheritance chain"},
	{"unravel <Name>       ", "Show fully resolved fields"},
	{"blame <Name>         ", "Git attribution per field item"},
	{"changelog            ", "Prompt-centric git change history"},
	// --- column 2: quality, CI, packs & integration ---
	{"inspect              ", "Validate your prompt library"},
	{"doctor               ", "Health checks + smell detection"},
	{"smells               ", "Standalone smell report"},
	{"audit                ", "Scan for dangerous instructions"},
	{"contract <Name>      ", "Print output contract"},
	{"check-output <N> <f> ", "Validate output file vs contract"},
	{"diff <A> <B>         ", "Field-aware diff between two prompts"},
	{"review               ", "PR-friendly summary of changes"},
	{"lock                 ", "Generate / update loom.lock"},
	{"check-lock           ", "Verify prompts match loom.lock"},
	{"ci                   ", "Run all CI gates"},
	{"test                 ", "Smoke-test prompts against AI model"},
	{"test --all           ", "Test all prompts with contracts"},
	{"graph                ", "Interactive dependency graph"},
	{"graph --format x     ", "Graph as mermaid or dot (plain)"},
	{"graph --unused       ", "Show unused blocks"},
	{"stats <Name>         ", "Per-field token estimates"},
	{"stats --all          ", "Token counts for all prompts"},
	{"pack build           ", "Bundle project into .lpack"},
	{"pack install <path>  ", "Install a .lpack archive"},
	{"pack list            ", "List installed packs"},
	{"mcp manifest         ", "Generate MCP tool manifest JSON"},
	{"lsp                  ", "Start LSP server (for editors)"},
	{"recipe list          ", "List built-in scaffolding recipes"},
	{"recipe apply <name>  ", "Scaffold a prompt library from recipe"},
	{"weave --interactive  ", "Guided prompt assembly wizard (TUI)"},
	{"playground <Name>    ", "Live preview playground (TUI)"},
	{"minimize             ", "Detect duplicate and contradictory items"},
	{"stale                ", "Find outdated version mentions"},
	{"todos                ", "List all todo: items in the library"},
	{"journal list         ", "List change journal entries"},
	{"journal add <msg>    ", "Add a change journal entry"},
}

// Banner builds the welcome screen string. When showCmds is false the command
// table is omitted, leaving only the logo, tagline, and library stats.
func Banner(version, cwd string, promptCount, blockCount, errCount int, showCmds bool) string {
	var b strings.Builder

	// Logo in primary purple.
	for _, line := range strings.Split(logoLines, "\n") {
		b.WriteString(BannerStyle.Render(line))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	// Tagline + version on one line.
	tagline := TaglineStyle.Render("Treat prompts like source code")
	version = VersionStyle.Render("v" + version)
	b.WriteString(fmt.Sprintf("  %s    %s\n", tagline, version))
	b.WriteByte('\n')

	// Library stats.
	if promptCount >= 0 {
		stats := statsLine(promptCount, blockCount, errCount)
		b.WriteString("  " + stats + "\n")
		b.WriteString("  " + MutedStyle.Render("project: ") + PathStyle.Render(filepath.Base(cwd)) + "\n")
		b.WriteByte('\n')
	}

	if !showCmds {
		hint := MutedStyle.Render("Commands hidden — type 'show' to reveal.")
		b.WriteString("  " + hint + "\n")
		return b.String()
	}

	// Two-column command table — keeps banner height ~17 rows instead of ~33.
	b.WriteString("  " + SubHeaderStyle.Render("Commands") + "\n")
	b.WriteString("  " + Divider(92) + "\n")
	half := (len(commandHelp) + 1) / 2

	// Compute the maximum visual width of the left cell so the │ divider
	// lands in the same column on every row.
	leftColWidth := 0
	for i := 0; i < half; i++ {
		w := len(commandHelp[i].cmd) + 2 + len(commandHelp[i].desc)
		if w > leftColWidth {
			leftColWidth = w
		}
	}

	colDiv := MutedStyle.Render("│")
	for i := 0; i < half; i++ {
		left := commandHelp[i]
		lCmd := CommandStyle.Render(left.cmd)
		lDesc := ArgDescStyle.Render(left.desc)
		// Pad to leftColWidth using the unstyled length so the divider aligns.
		visualWidth := len(left.cmd) + 2 + len(left.desc)
		pad := strings.Repeat(" ", leftColWidth-visualWidth)
		leftCell := fmt.Sprintf("%s  %s%s", lCmd, lDesc, pad)

		if i+half < len(commandHelp) {
			right := commandHelp[i+half]
			rCmd := CommandStyle.Render(right.cmd)
			rDesc := ArgDescStyle.Render(right.desc)
			b.WriteString(fmt.Sprintf("  %s  %s  %s  %s\n", leftCell, colDiv, rCmd, rDesc))
		} else {
			b.WriteString(fmt.Sprintf("  %s\n", leftCell))
		}
	}
	b.WriteByte('\n')

	// Usage hint.
	hint := MutedStyle.Render("Type a command and press Enter.  Tab to complete.  Ctrl+C to quit.")
	b.WriteString("  " + hint + "\n")

	return b.String()
}

func statsLine(prompts, blocks, errs int) string {
	ps := fmt.Sprintf("%d prompts", prompts)
	bs := fmt.Sprintf("%d blocks", blocks)

	var errPart string
	if errs == 0 {
		errPart = SuccessStyle.Render("✓ no errors")
	} else {
		errPart = ErrorStyle.Render(fmt.Sprintf("✗ %d errors", errs))
	}

	return PromptNameStyle.Render(ps) +
		MutedStyle.Render("  ·  ") +
		BlockNameStyle.Render(bs) +
		MutedStyle.Render("  ·  ") +
		errPart
}
