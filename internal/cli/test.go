package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/testrunner"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test [Name]",
	Short: "Smoke-test prompts against a real AI model and assert contract",
	Long: `Sends a rendered prompt to a configured AI model with a test fixture (or built-in
stub) and checks the response against the prompt's declared contract block.

Requires an API key set via the environment variable configured in loom.toml
(default: $GEMINI_API_KEY for Gemini, $ANTHROPIC_API_KEY for Anthropic).

Examples:
  loom test SecurityReviewer
  loom test --all
  loom test CodeReviewer --model gemini-1.5-flash
  loom test CodeReviewer --record
  loom test CodeReviewer --compare`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTest,
}

var (
	testAllFlag     bool
	testModelFlag   string
	testRecordFlag  bool
	testCompareFlag bool
)

func init() {
	testCmd.Flags().BoolVar(&testAllFlag, "all", false, "run tests for all prompts")
	testCmd.Flags().StringVar(&testModelFlag, "model", "", "model override (e.g. gemini-2.0-flash)")
	testCmd.Flags().BoolVar(&testRecordFlag, "record", false, "record response as baseline for future --compare runs")
	testCmd.Flags().BoolVar(&testCompareFlag, "compare", false, "compare response against recorded baseline")
}

func runTest(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !testAllFlag {
		return fmt.Errorf("specify a prompt name or use --all")
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	opts := testrunner.Options{
		Model:   testModelFlag,
		Record:  testRecordFlag,
		Compare: testCompareFlag,
	}

	var results []testrunner.Result
	spinMsg := "running tests…"
	if len(args) == 1 {
		spinMsg = fmt.Sprintf("testing %s…", args[0])
	}

	_, _ = tui.RunWithSpinner(spinMsg, func() (string, error) {
		if testAllFlag {
			results = testrunner.RunAll(reg, cfg, cwd, opts)
		} else {
			results = []testrunner.Result{testrunner.Run(args[0], reg, cfg, cwd, opts)}
		}
		return "", nil
	})

	printTestResults(results)

	for _, r := range results {
		if r.Err != nil || (!r.Passed && !r.Skipped) {
			os.Exit(1)
		}
	}
	return nil
}

func printTestResults(results []testrunner.Result) {
	sep := tui.MutedStyle.Render(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom test"))
	fmt.Println()
	fmt.Println("  " + sep)
	fmt.Println()

	passed, failed, skipped := 0, 0, 0
	maxName := 0
	for _, r := range results {
		if len(r.PromptName) > maxName {
			maxName = len(r.PromptName)
		}
	}
	if maxName < 20 {
		maxName = 20
	}

	for _, r := range results {
		pad := strings.Repeat(" ", maxName-len(r.PromptName))
		name := tui.PromptNameStyle.Render(r.PromptName) + pad

		switch {
		case r.Err != nil:
			failed++
			icon := tui.ErrorStyle.Render("✗")
			detail := tui.MutedStyle.Render("error: "+r.Err.Error())
			fmt.Printf("  %s  %s  %s\n", icon, name, detail)

		case r.Skipped:
			skipped++
			icon := tui.MutedStyle.Render("—")
			detail := tui.MutedStyle.Render("("+r.SkipReason+")")
			fmt.Printf("  %s  %s  %s\n", icon, name, detail)

		case r.Passed:
			passed++
			icon := tui.SuccessStyle.Render("✓")
			dur := tui.MutedStyle.Render(formatDuration(r.Duration))
			extra := ""
			if testRecordFlag {
				extra = "  " + tui.MutedStyle.Render("baseline recorded")
			}
			fmt.Printf("  %s  %s  contract passed  %s%s\n", icon, name, dur, extra)

		default:
			failed++
			icon := tui.ErrorStyle.Render("✗")
			dur := tui.MutedStyle.Render(formatDuration(r.Duration))
			fmt.Printf("  %s  %s  %s\n", icon, name, dur)
			for _, f := range r.Failures {
				fmt.Printf("       %s %s\n", tui.MutedStyle.Render("→"), f.Detail)
			}
		}
	}

	fmt.Println()
	fmt.Println("  " + sep)
	total := passed + failed
	summary := fmt.Sprintf("Passed: %d / %d", passed, total)
	if skipped > 0 {
		summary += fmt.Sprintf("  (skipped: %d)", skipped)
	}
	if failed == 0 {
		fmt.Println("  " + tui.SuccessStyle.Render(summary))
	} else {
		fmt.Println("  " + tui.ErrorStyle.Render(summary))
	}
	fmt.Println()
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("(%dms)", d.Milliseconds())
	}
	return fmt.Sprintf("(%.1fs)", d.Seconds())
}
