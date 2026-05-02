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
	{"deploy               ", "Write configured deploy targets"},
	{"deploy --dry-run     ", "Preview target writes"},
	{"thread prompt <Name> ", "Scaffold a new prompt file"},
	{"fmt                  ", "Format prompt files"},
	{"list                 ", "List prompts, blocks, overlays"},
	{"trace <Name>         ", "Show inheritance chain"},
	{"unravel <Name>       ", "Show fully resolved fields"},
	// --- column 2: quality & CI ---
	{"inspect              ", "Validate your prompt library"},
	{"doctor               ", "Health checks + smell detection"},
	{"doctor <Name>        ", "Check one prompt's health"},
	{"smells               ", "Standalone smell report"},
	{"contract <Name>      ", "Print output contract"},
	{"check-output <N> <f> ", "Validate output file vs contract"},
	{"lock                 ", "Generate / update loom.lock"},
	{"check-lock           ", "Verify prompts match loom.lock"},
	{"ci                   ", "Run all CI gates"},
	{"fingerprint <Name>   ", "Print the prompt fingerprint"},
	{"diff <A> <B>         ", "Field-aware diff between two prompts"},
	{"diff <Name> --dist   ", "Diff prompt vs its dist file"},
	{"review               ", "PR-friendly summary of changes"},
	{"trace --field x      ", "Trace one resolved field"},
	{"trace --tree         ", "Show the full project tree"},
	{"deploy --diff        ", "Show content diffs before writing"},
}

// Banner builds the full welcome screen string.
func Banner(version, cwd string, promptCount, blockCount, errCount int) string {
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

	// Two-column command table — keeps banner height ~17 rows instead of ~33.
	b.WriteString("  " + SubHeaderStyle.Render("Commands") + "\n")
	b.WriteString("  " + Divider(92) + "\n")
	half := (len(commandHelp) + 1) / 2
	for i := 0; i < half; i++ {
		left := commandHelp[i]
		lCmd := CommandStyle.Render(left.cmd)
		lDesc := ArgDescStyle.Render(left.desc)
		leftCell := fmt.Sprintf("%s  %s", lCmd, lDesc)

		if i+half < len(commandHelp) {
			right := commandHelp[i+half]
			rCmd := CommandStyle.Render(right.cmd)
			rDesc := ArgDescStyle.Render(right.desc)
			b.WriteString(fmt.Sprintf("  %s    %s  %s\n", leftCell, rCmd, rDesc))
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
