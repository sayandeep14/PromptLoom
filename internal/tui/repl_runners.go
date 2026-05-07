package tui

// repl_runners.go — executor wrappers for commands that were added after the
// initial REPL dispatch table was written (M16–M21: test, blame, changelog,
// audit, diff, review, mcp, import, lsp notice, recipe).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	iaudit "github.com/sayandeepgiri/promptloom/internal/audit"
	iast "github.com/sayandeepgiri/promptloom/internal/ast"
	iblame "github.com/sayandeepgiri/promptloom/internal/blame"
	"github.com/sayandeepgiri/promptloom/internal/importer"
	ijournal "github.com/sayandeepgiri/promptloom/internal/journal"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	imcp "github.com/sayandeepgiri/promptloom/internal/mcp"
	"github.com/sayandeepgiri/promptloom/internal/minimize"
	"github.com/sayandeepgiri/promptloom/internal/recipe"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	istale "github.com/sayandeepgiri/promptloom/internal/stale"
	itestrunner "github.com/sayandeepgiri/promptloom/internal/testrunner"
)

// ---- test ----

func RunTestREPL(name string, all bool, model string, cwd string) (string, bool) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	opts := itestrunner.Options{Model: model}
	var results []itestrunner.Result
	if all {
		results = itestrunner.RunAll(reg, cfg, cwd, opts)
	} else {
		results = []itestrunner.Result{itestrunner.Run(name, reg, cfg, cwd, opts)}
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 50))
	b.WriteString("\n  " + HeaderStyle.Render("loom test") + "\n\n")
	b.WriteString("  " + sep + "\n\n")

	passed, failed, skipped := 0, 0, 0
	for _, r := range results {
		if r.Skipped {
			skipped++
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				MutedStyle.Render(fmt.Sprintf("%-36s", r.PromptName)),
				MutedStyle.Render("SKIP (no contract)")))
			continue
		}
		if r.Err != nil {
			failed++
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				ErrorStyle.Render(fmt.Sprintf("%-36s", r.PromptName)),
				ErrorStyle.Render("ERROR")))
			b.WriteString("  " + MutedStyle.Render("  "+r.Err.Error()) + "\n\n")
			continue
		}
		if r.Passed {
			passed++
			dur := replFormatDuration(r.Duration)
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				SuccessStyle.Render(fmt.Sprintf("%-36s", r.PromptName)),
				SuccessStyle.Render("PASS"),
				MutedStyle.Render(dur)))
		} else {
			failed++
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				ErrorStyle.Render(fmt.Sprintf("%-36s", r.PromptName)),
				ErrorStyle.Render("FAIL")))
		}
	}

	b.WriteString("\n  " + sep + "\n")
	summary := fmt.Sprintf("  %s passed  %s failed  %s skipped\n",
		SuccessStyle.Render(fmt.Sprintf("%d", passed)),
		ErrorStyle.Render(fmt.Sprintf("%d", failed)),
		MutedStyle.Render(fmt.Sprintf("%d", skipped)))
	b.WriteString(summary)
	b.WriteByte('\n')
	return b.String(), failed > 0
}

func replFormatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// ---- blame ----

func RunBlameREPL(name, field, since, instruction, cwd string) (string, bool) {
	results, err := iblame.RunBlame(name, field, since, instruction, cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 57))
	b.WriteString("\n  " + HeaderStyle.Render("loom blame  "+name) + "\n\n")

	for _, r := range results {
		b.WriteString("  " + SubHeaderStyle.Render(r.Field) + "\n")
		b.WriteString("  " + sep + "\n")
		for _, item := range r.Items {
			val := item.Value
			if len(val) > 80 {
				val = val[:77] + "…"
			}
			b.WriteString(fmt.Sprintf("  %s %s\n",
				MutedStyle.Render("●"),
				TextStyle.Render(fmt.Sprintf("%q", val))))
			b.WriteString(fmt.Sprintf("    %s %s  %s %d\n",
				MutedStyle.Render("from:"),
				PathStyle.Render(item.File),
				MutedStyle.Render("line"),
				item.Line))
			if item.Untracked {
				b.WriteString(fmt.Sprintf("    %s\n", MutedStyle.Render("(untracked)")))
			} else if item.Commit.Hash != "" {
				b.WriteString(fmt.Sprintf("    %s %s  %s  %s\n",
					MutedStyle.Render("commit:"),
					PromptNameStyle.Render(item.Commit.Hash[:7]),
					MutedStyle.Render("by "+item.Commit.Author),
					MutedStyle.Render(item.Commit.Summary)))
			}
			b.WriteByte('\n')
		}
	}
	return b.String(), false
}

// ---- changelog ----

func RunChangelogREPL(name, since, cwd string) (string, bool) {
	changelogs, err := iblame.BuildChangelog(cwd, since, name)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	if len(changelogs) == 0 {
		return MutedStyle.Render("  No changelog entries found.\n"), false
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 50))
	b.WriteString("\n  " + HeaderStyle.Render("Prompt Changelog") + "\n\n")

	for _, cl := range changelogs {
		b.WriteString("  " + PromptNameStyle.Render(cl.Name) + "\n")
		b.WriteString("  " + sep + "\n")
		for _, e := range cl.Entries {
			date := e.Date.Format("2006-01-02")
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
				MutedStyle.Render(date),
				TextStyle.Render(e.Message),
				MutedStyle.Render("("+e.Author+")")))
		}
		b.WriteByte('\n')
	}
	return b.String(), false
}

// ---- audit ----

func RunAuditREPL(name string, all bool, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	type auditResult struct {
		name     string
		findings []iaudit.Finding
		err      error
	}

	var results []auditResult
	if all {
		for _, node := range reg.Prompts() {
			rp, err := resolve.Resolve(node.Name, reg)
			if err != nil {
				results = append(results, auditResult{name: node.Name, err: err})
				continue
			}
			results = append(results, auditResult{name: node.Name, findings: iaudit.Audit(rp)})
		}
	} else {
		rp, err := resolve.Resolve(name, reg)
		if err != nil {
			results = append(results, auditResult{name: name, err: err})
		} else {
			results = append(results, auditResult{name: name, findings: iaudit.Audit(rp)})
		}
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 57))
	b.WriteString("\n  " + HeaderStyle.Render("loom audit") + "\n\n")
	b.WriteString("  " + sep + "\n\n")

	hasHigh := false
	allClean := true
	for _, r := range results {
		if r.err != nil {
			allClean = false
			b.WriteString(fmt.Sprintf("  %s  ERROR\n", ErrorStyle.Render(fmt.Sprintf("%-40s", r.name))))
			b.WriteString("  " + MutedStyle.Render("  "+r.err.Error()) + "\n\n")
			continue
		}
		if len(r.findings) == 0 {
			b.WriteString(fmt.Sprintf("  %s  PASS\n", SuccessStyle.Render(fmt.Sprintf("%-40s", r.name))))
			continue
		}
		allClean = false
		b.WriteString(fmt.Sprintf("  %s  FAIL\n", ErrorStyle.Render(fmt.Sprintf("%-40s", r.name))))
		b.WriteString("  " + sep + "\n")
		for _, f := range r.findings {
			if f.Risk == iaudit.High {
				hasHigh = true
			}
			riskStr := auditRiskStyle(f.Risk).Render(fmt.Sprintf("[%s]", f.Risk.String()))
			val := f.Value
			if len(val) > 60 {
				val = val[:57] + "…"
			}
			b.WriteString(fmt.Sprintf("  %s   %s %s\n",
				riskStr,
				MutedStyle.Render(f.Field+":"),
				TextStyle.Render(fmt.Sprintf("%q", val))))
			b.WriteString(fmt.Sprintf("           %s %s\n", MutedStyle.Render("Reason:"), f.Reason))
			b.WriteString(fmt.Sprintf("           %s %s\n", MutedStyle.Render("Fix:"), f.Fix))
			b.WriteByte('\n')
		}
	}

	b.WriteString("  " + sep + "\n")
	if allClean {
		b.WriteString("  " + SuccessStyle.Render("All prompts passed audit.") + "\n")
	}
	b.WriteByte('\n')
	return b.String(), hasHigh
}

func auditRiskStyle(r iaudit.RiskLevel) lipgloss.Style {
	switch r {
	case iaudit.High:
		return ErrorStyle
	case iaudit.Medium:
		return WarningStyle
	default:
		return MutedStyle
	}
}

// ---- diff ----

func RunDiffREPL(a, b string, opts DiffOptions, cwd string) (string, bool) {
	out, hasChanges, err := RunDiff(a, b, opts, cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return out, hasChanges
}

// ---- review ----

func RunReviewREPL(since, cwd string) (string, bool) {
	out, err := RunReview(since, cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return out, false
}

// ---- mcp ----

func RunMCPManifestREPL(name string, all bool, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var manifest *imcp.Manifest
	var warnings []string
	if all {
		manifest, warnings, err = imcp.GenerateAll(reg)
	} else {
		manifest, warnings, err = imcp.Generate(name, reg)
	}
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	if manifest == nil {
		return ErrorStyle.Render(fmt.Sprintf("Error: prompt %q not found\n", name)), true
	}

	var b strings.Builder
	for _, w := range warnings {
		b.WriteString("  " + WarningStyle.Render("warn: "+w) + "\n")
	}
	data, _ := json.MarshalIndent(manifest, "  ", "  ")
	b.WriteString("  ")
	b.Write(data)
	b.WriteByte('\n')
	return b.String(), false
}

// ---- import ----

func RunImportREPL(path, name, outDir string, force bool, cwd string) (string, bool) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	if name == "" {
		name = importer.NameFromPath(path)
	}
	if outDir == "" {
		outDir = filepath.Join(cwd, "prompts")
	} else if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cwd, outDir)
	}

	result := importer.Import(string(src), name)

	var b strings.Builder
	for _, w := range result.Warnings {
		b.WriteString("  " + WarningStyle.Render("warn: "+w) + "\n")
	}

	outPath := filepath.Join(outDir, name+".prompt.loom")
	if _, err := os.Stat(outPath); err == nil && !force {
		b.WriteString(MutedStyle.Render(fmt.Sprintf("  skip %s (already exists; use --force to overwrite)\n", outPath)))
		return b.String(), false
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	if err := os.WriteFile(outPath, []byte(result.DSL), 0o644); err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	b.WriteString(SuccessStyle.Render("  wrote ") + PathStyle.Render(outPath) + "\n")

	// Run inspect to surface any issues.
	b.WriteByte('\n')
	inspectOut, _ := RunInspect(cwd)
	b.WriteString(inspectOut)
	return b.String(), false
}

// ---- recipe ----

func RunRecipeListREPL() (string, bool) {
	recipes := recipe.List()
	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Built-in Recipes") + "\n\n")
	for _, r := range recipes {
		b.WriteString("  " + PromptNameStyle.Render(fmt.Sprintf("%-22s", r.Name)) + "\n")
		b.WriteString("    " + MutedStyle.Render(r.Description) + "\n")
		if len(r.Flags) > 0 {
			b.WriteString("    " + MutedStyle.Render("flags: "+strings.Join(r.Flags, "  ")) + "\n")
		}
		b.WriteByte('\n')
	}
	b.WriteString("  " + MutedStyle.Render("Usage: recipe apply <name> [--language x] [--framework y]") + "\n\n")
	return b.String(), false
}

func RunRecipeApplyREPL(name, language, framework, style string, force bool, cwd string) (string, bool) {
	opts := recipe.Options{Language: language, Framework: framework, Style: style}
	result, err := recipe.Apply(name, opts, cwd, force)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("recipe apply  "+name) + "\n\n")
	for _, f := range result.Written {
		b.WriteString("  " + SuccessStyle.Render("  wrote ") + PathStyle.Render(f) + "\n")
	}
	for _, f := range result.Skipped {
		b.WriteString("  " + MutedStyle.Render("  skip  ") + PathStyle.Render(f) + MutedStyle.Render(" (exists)") + "\n")
	}
	b.WriteString(fmt.Sprintf("\n  %s written, %s skipped\n\n",
		SuccessStyle.Render(fmt.Sprintf("%d", len(result.Written))),
		MutedStyle.Render(fmt.Sprintf("%d", len(result.Skipped)))))

	if len(result.Written) > 0 {
		inspectOut, _ := RunInspect(cwd)
		b.WriteString(inspectOut)
	}
	return b.String(), false
}

// ---- minimize ----

func RunMinimizeREPL(promptFilter []string, threshold float64, apply bool, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	opts := minimize.Options{NearDupThreshold: threshold}
	filter := ""
	if len(promptFilter) > 0 {
		filter = promptFilter[0]
	}

	var allIssues []minimize.Issue
	for _, node := range reg.Prompts() {
		if filter != "" && node.Name != filter {
			continue
		}
		rp, err := resolve.Resolve(node.Name, reg)
		if err != nil {
			continue
		}
		allIssues = append(allIssues, minimize.Analyze(rp, opts)...)
		if apply {
			minimize.Apply(rp, opts)
		}
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("loom minimize") + "\n\n")
	if len(allIssues) == 0 {
		b.WriteString("  " + SuccessStyle.Render("No redundancy found.") + "\n\n")
		return b.String(), false
	}

	exact, near, contradictions := 0, 0, 0
	for _, iss := range allIssues {
		switch iss.Kind {
		case minimize.KindExactDuplicate:
			exact++
			b.WriteString(fmt.Sprintf("  %s  %s.%s: %q\n",
				ErrorStyle.Render("[exact-dup]"),
				PromptNameStyle.Render(iss.Prompt), MutedStyle.Render(iss.Field),
				truncateStr(iss.ItemA, 60)))
		case minimize.KindNearDuplicate:
			near++
			b.WriteString(fmt.Sprintf("  %s  %s.%s  (%.0f%%)\n",
				WarningStyle.Render("[near-dup] "),
				PromptNameStyle.Render(iss.Prompt), MutedStyle.Render(iss.Field),
				iss.Similarity*100))
			b.WriteString(fmt.Sprintf("    A: %s\n", MutedStyle.Render(truncateStr(iss.ItemA, 70))))
			b.WriteString(fmt.Sprintf("    B: %s\n", MutedStyle.Render(truncateStr(iss.ItemB, 70))))
		case minimize.KindContradiction:
			contradictions++
			b.WriteString(fmt.Sprintf("  %s  %s.%s\n",
				ErrorStyle.Render("[contradict]"),
				PromptNameStyle.Render(iss.Prompt), MutedStyle.Render(iss.Field)))
		}
	}
	b.WriteString(fmt.Sprintf("\n  %s exact  %s near-dup  %s contradictions\n\n",
		ErrorStyle.Render(fmt.Sprintf("%d", exact)),
		WarningStyle.Render(fmt.Sprintf("%d", near)),
		ErrorStyle.Render(fmt.Sprintf("%d", contradictions))))
	return b.String(), false
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// ---- stale ----

func RunStaleREPL(filter, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	deps, err := istale.ScanDeps(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("loom stale") + "\n\n")

	if len(deps) == 0 {
		b.WriteString("  " + MutedStyle.Render("No dependency files found.") + "\n\n")
		return b.String(), false
	}

	var prompts []*iast.ResolvedPrompt
	for _, node := range reg.Prompts() {
		if filter != "" && node.Name != filter {
			continue
		}
		rp, err := resolve.Resolve(node.Name, reg)
		if err != nil {
			continue
		}
		prompts = append(prompts, rp)
	}

	findings, err := istale.Scan(prompts, cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	if len(findings) == 0 {
		b.WriteString("  " + SuccessStyle.Render("All version mentions are current.") + "\n\n")
		return b.String(), false
	}

	for _, f := range findings {
		b.WriteString(fmt.Sprintf("  %s  %s.%s\n",
			WarningStyle.Render("[stale]"),
			PromptNameStyle.Render(f.Prompt), MutedStyle.Render(f.Field)))
		b.WriteString(fmt.Sprintf("    %s declares %s → prompt mentions %s\n\n",
			MutedStyle.Render(f.Dep),
			SuccessStyle.Render(f.Version),
			WarningStyle.Render(f.Mention)))
	}
	return b.String(), false
}

// ---- todos ----

func RunTodosREPL(filter, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("loom todos") + "\n\n")
	total := 0

	for _, node := range reg.Prompts() {
		if filter != "" && node.Name != filter {
			continue
		}
		todos := todoItems(node.Fields)
		if len(todos) == 0 {
			continue
		}
		b.WriteString("  " + PromptNameStyle.Render(node.Name) + MutedStyle.Render("  (prompt)") + "\n")
		for _, item := range todos {
			b.WriteString(fmt.Sprintf("    %s %s\n", MutedStyle.Render("▸"), item))
			total++
		}
		b.WriteByte('\n')
	}

	for _, node := range reg.Blocks() {
		if filter != "" && node.Name != filter {
			continue
		}
		todos := todoItems(node.Fields)
		if len(todos) == 0 {
			continue
		}
		b.WriteString("  " + PromptNameStyle.Render(node.Name) + MutedStyle.Render("  (block)") + "\n")
		for _, item := range todos {
			b.WriteString(fmt.Sprintf("    %s %s\n", MutedStyle.Render("▸"), item))
			total++
		}
		b.WriteByte('\n')
	}

	if total == 0 {
		b.WriteString("  " + MutedStyle.Render("No todo items found.") + "\n\n")
	} else {
		b.WriteString(fmt.Sprintf("  %s todo items total.\n\n",
			PromptNameStyle.Render(fmt.Sprintf("%d", total))))
	}
	return b.String(), false
}

func todoItems(fields []iast.FieldOperation) []string {
	var out []string
	for _, f := range fields {
		if f.FieldName == "todo" {
			for _, v := range f.Value {
				out = append(out, strings.TrimPrefix(v, "- "))
			}
		}
	}
	return out
}

// ---- journal ----

func RunJournalListREPL(filter, cwd string) (string, bool) {
	var entries []ijournal.Entry
	var err error
	if filter != "" {
		entries, err = ijournal.ForPrompt(cwd, filter)
	} else {
		entries, err = ijournal.List(cwd)
	}
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("loom journal") + "\n\n")

	if len(entries) == 0 {
		b.WriteString("  " + MutedStyle.Render("No journal entries. Use: journal add <message>") + "\n\n")
		return b.String(), false
	}

	for _, e := range entries {
		date := e.Date.Format("2006-01-02")
		prompt := ""
		if e.Prompt != "" {
			prompt = "  " + MutedStyle.Render("["+e.Prompt+"]")
		}
		b.WriteString(fmt.Sprintf("  %s  %s%s\n",
			MutedStyle.Render(date),
			PromptNameStyle.Render(e.Message),
			prompt))
	}
	b.WriteString(fmt.Sprintf("\n  %s entries\n\n", MutedStyle.Render(fmt.Sprintf("%d", len(entries)))))
	return b.String(), false
}

func RunJournalAddREPL(message, prompt, author, body, cwd string) (string, bool) {
	if message == "" {
		return ErrorStyle.Render("Usage: journal add <message>\n"), true
	}
	path, err := ijournal.Add(cwd, message, prompt, author, body)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return "\n  " + SuccessStyle.Render("Journal entry created") + "\n  " + PathStyle.Render(path) + "\n\n", false
}
