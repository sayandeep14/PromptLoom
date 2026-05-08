package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/secret"
	"github.com/sayandeepgiri/promptloom/internal/starter"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/sayandeepgiri/promptloom/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	startNoLLM  bool
	startMinimal bool
	startBest    bool
	startStack   string
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Generate a starter prompt library for your project",
	Long: `loom start reads CLAUDE.md (required) and TODO.md (optional) from your project,
then generates a tailored prompt library using an LLM.

Requires loom.toml to be present (run 'loom init' first).

Flags:
  --nollm     Detect tech stack and use built-in templates (no LLM call)
  --minimal   Smaller prompt set with reduced token spend
  --best      Most comprehensive library, maximum quality (higher token cost)
  --stack     Override stack detection (e.g. go, python, java-spring, typescript)`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().BoolVar(&startNoLLM, "nollm", false, "skip LLM; use built-in stack templates")
	startCmd.Flags().BoolVar(&startMinimal, "minimal", false, "minimal token spend — 3-5 prompts only")
	startCmd.Flags().BoolVar(&startBest, "best", false, "highest quality — larger LLM call")
	startCmd.Flags().StringVar(&startStack, "stack", "", "override stack detection (go, python, typescript, java-spring, rust, ...)")
}

func runStart(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("loom.toml not found — run 'loom init' first\n  (%w)", err)
	}

	secret.Load(cwd)

	// --- 1. Scan workspace ---
	fmt.Println()
	fmt.Println(tui.HeaderStyle.Render("  Scanning workspace…"))

	info, err := workspace.Scan(cwd)
	if err != nil {
		return fmt.Errorf("workspace scan failed: %w", err)
	}

	// Override stack if requested.
	if startStack != "" {
		info.Stack = workspace.Stack(startStack)
		info.Language = stackToLanguage(startStack)
	}

	// Validate required files.
	if !info.HasClaudeMD {
		return fmt.Errorf(`CLAUDE.md not found in %s

  loom start uses CLAUDE.md as the primary source of project context.
  Create it with a description of your codebase, tech stack, and conventions,
  then run 'loom start' again.

  Tip: Claude Code can generate one for you — run /init inside Claude Code.`, cwd)
	}

	// --- 2. Print detected context ---
	fmt.Println()
	fmt.Println(tui.SubHeaderStyle.Render("  Detected project:"))
	fmt.Println(tui.DividerStyle.Render("  " + strings.Repeat("─", 45)))
	fmt.Print(tui.TextStyle.Render(info.Summary()))
	if !info.HasTodoMD {
		fmt.Println(tui.MutedStyle.Render("  TODO.md:    not found (optional — will be skipped)"))
	} else {
		fmt.Println(tui.SuccessStyle.Render("  TODO.md:    found"))
	}
	fmt.Println()

	// --- 3. Generate plan ---
	tier := starter.TierDefault
	mode := "moderate"
	if startMinimal {
		tier = starter.TierMinimal
		mode = "minimal"
	} else if startBest {
		tier = starter.TierBest
		mode = "best"
	}

	var plan *starter.Plan

	if startNoLLM {
		fmt.Println(tui.MutedStyle.Render("  Mode: --nollm (using built-in templates)"))
		fmt.Println()
		plan = starter.TemplatesForStack(info, tier)
	} else {
		fmt.Printf("%s\n\n", tui.MutedStyle.Render(fmt.Sprintf("  Mode: LLM generation (%s token budget)", mode)))
		fmt.Println(tui.MutedStyle.Render("  Calling LLM — this may take 10-30 seconds…"))
		plan, err = starter.GenerateLLM(info, cfg, tier)
		if err != nil {
			return fmt.Errorf("LLM generation failed: %w\n\nTip: run with --nollm to use built-in templates without an API key", err)
		}
	}

	// --- 4. Show plan ---
	printPlan(plan, cfg, cwd)

	// --- 5. Confirm / edit / cancel ---
	plan, err = confirmPlan(plan)
	if err != nil {
		return err
	}
	if plan == nil {
		fmt.Println(tui.MutedStyle.Render("\n  Aborted."))
		return nil
	}

	// --- 6. Write files ---
	written, err := starter.Write(plan, cfg, cwd)
	if err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	// --- 7. Summary ---
	fmt.Println()
	fmt.Println(tui.SubHeaderStyle.Render("  Generated starter pack:"))
	fmt.Println(tui.DividerStyle.Render("  " + strings.Repeat("─", 45)))

	promptsDir := filepath.Join(cwd, cfg.Paths.Prompts)
	blocksDir := filepath.Join(cwd, cfg.Paths.Blocks)
	printGroup("prompts/", written, promptsDir)
	printGroup("blocks/", written, blocksDir)

	fmt.Println()
	fmt.Println(tui.SubHeaderStyle.Render("  Next steps:"))
	fmt.Println(tui.CommandStyle.Render("    loom inspect"))
	fmt.Println(tui.CommandStyle.Render("    loom weave --all"))
	fmt.Println(tui.CommandStyle.Render("    loom list"))
	fmt.Println()

	return nil
}

// printPlan renders the plan as a human-readable table before confirmation.
func printPlan(plan *starter.Plan, cfg *config.Config, cwd string) {
	promptsDir := filepath.Join(cwd, cfg.Paths.Prompts)
	blocksDir := filepath.Join(cwd, cfg.Paths.Blocks)

	fmt.Println(tui.SubHeaderStyle.Render("  Generated starter pack plan:"))
	fmt.Println(tui.DividerStyle.Render("  " + strings.Repeat("─", 45)))

	var prompts, blocks []starter.File
	for _, f := range plan.Files {
		if f.Type == "block" {
			blocks = append(blocks, f)
		} else {
			prompts = append(prompts, f)
		}
	}

	if len(prompts) > 0 {
		fmt.Printf("  %s\n", tui.BrightStyle.Render(relPath(cwd, promptsDir)+"/"))
		for _, f := range prompts {
			fmt.Printf("    %s  %s\n",
				tui.PathStyle.Render(f.Name),
				tui.MutedStyle.Render("— "+f.Description),
			)
		}
	}
	if len(blocks) > 0 {
		fmt.Printf("  %s\n", tui.BrightStyle.Render(relPath(cwd, blocksDir)+"/"))
		for _, f := range blocks {
			fmt.Printf("    %s  %s\n",
				tui.BlockNameStyle.Render(f.Name),
				tui.MutedStyle.Render("— "+f.Description),
			)
		}
	}
	fmt.Println()
}

// confirmPlan prompts the user to accept, edit, or cancel the plan.
// Returns (nil, nil) on cancel.
func confirmPlan(plan *starter.Plan) (*starter.Plan, error) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [%s/%s/%s] ",
			tui.BrightStyle.Render("  Generate?"),
			tui.SuccessStyle.Render("y"),
			tui.WarningStyle.Render("e=edit"),
			tui.MutedStyle.Render("n"),
		)
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("input error: %w", err)
		}
		answer := strings.TrimSpace(strings.ToLower(line))

		switch answer {
		case "y", "yes", "":
			return plan, nil
		case "n", "no", "q", "quit":
			return nil, nil
		case "e", "edit":
			edited, err := editPlan(plan)
			if err != nil {
				fmt.Printf("%s\n", tui.ErrorStyle.Render("  Edit failed: "+err.Error()))
				continue
			}
			printPlan(edited, &config.Config{
				Paths: config.Paths{Prompts: "prompts", Blocks: "blocks"},
			}, ".")
			plan = edited
		default:
			fmt.Printf("%s\n", tui.MutedStyle.Render("  Please enter y, e (edit), or n."))
		}
	}
}

// editPlan writes the plan to a temp file, opens $EDITOR, and re-parses.
func editPlan(plan *starter.Plan) (*starter.Plan, error) {
	f, err := os.CreateTemp("", "loom-start-plan-*.md")
	if err != nil {
		return nil, fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := f.Name()
	defer os.Remove(tmpPath)

	if _, err := f.WriteString(starter.FormatPlan(plan)); err != nil {
		f.Close()
		return nil, fmt.Errorf("could not write plan: %w", err)
	}
	f.Close()

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	fmt.Printf("%s\n", tui.MutedStyle.Render("  Opening plan in "+editor+"…"))
	editorCmd := exec.Command(editor, tmpPath) //nolint:gosec
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("could not read edited plan: %w", err)
	}

	parsed, err := starter.ParsePlan(string(data))
	if err != nil {
		return nil, fmt.Errorf("could not parse edited plan: %w", err)
	}
	return parsed, nil
}

// printGroup prints the files that live under dir.
func printGroup(label string, written []string, dir string) {
	var filtered []string
	for _, p := range written {
		if strings.HasPrefix(filepath.Clean(p), filepath.Clean(dir)) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return
	}
	fmt.Printf("  %s\n", tui.BrightStyle.Render(label))
	for _, p := range filtered {
		fmt.Printf("    %s  %s\n",
			tui.SuccessStyle.Render("✓"),
			tui.PathStyle.Render(filepath.Base(p)),
		)
	}
}

func relPath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

func stackToLanguage(s string) string {
	m := map[string]string{
		"go":           "Go",
		"python":       "Python",
		"typescript":   "TypeScript",
		"javascript":   "JavaScript",
		"rust":         "Rust",
		"java":         "Java",
		"java-spring":  "Java",
		"spring-boot":  "Java",
	}
	if lang, ok := m[strings.ToLower(s)]; ok {
		return lang
	}
	return s
}
