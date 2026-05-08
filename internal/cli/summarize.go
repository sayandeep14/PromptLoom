package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/secret"
	"github.com/sayandeepgiri/promptloom/internal/summarize"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var (
	summarizeSaveFlag bool
	summarizeOutFlag  string
)

var summarizeCmd = &cobra.Command{
	Use:   "summarize <workspace | path...>",
	Short: "Generate an LLM-powered summary of your project or specific files",
	Long: `loom summarize generates a structured Markdown summary using an LLM.

Usage:
  loom summarize workspace          # whole-project architecture summary
  loom summarize src/               # specific directory
  loom summarize main.go utils.go   # specific files
  loom summarize src/ tests/        # multiple paths

The 'workspace' keyword triggers an architecture-level summary and saves it to
.loom/context/architecture-summary.md (use --save for other targets too).

Flags:
  --save       Write output to .loom/context/<name>-summary.md
  --out <path> Write output to a specific file`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSummarize,
}

func init() {
	summarizeCmd.Flags().BoolVar(&summarizeSaveFlag, "save", false, "save output to .loom/context/")
	summarizeCmd.Flags().StringVar(&summarizeOutFlag, "out", "", "write output to this file path")
}

func runSummarize(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return fmt.Errorf("loom.toml not found — run 'loom init' first\n  (%w)", err)
	}
	secret.Load(cwd)

	opts := summarize.Options{
		Save:       summarizeSaveFlag,
		OutputPath: summarizeOutFlag,
	}

	isWorkspace := len(args) == 1 && strings.ToLower(args[0]) == "workspace"

	fmt.Println()
	if isWorkspace {
		fmt.Println(tui.HeaderStyle.Render("  Summarizing workspace…"))
		fmt.Println(tui.MutedStyle.Render("  Calling LLM — this may take 20-40 seconds…"))
		fmt.Println()
		opts.Save = true // workspace always saves
		result, err := summarize.SummarizeWorkspace(cwd, cfg, opts)
		if err != nil {
			return fmt.Errorf("summarize failed: %w", err)
		}
		printSummarizeResult(result)
		return nil
	}

	fmt.Printf("%s\n", tui.HeaderStyle.Render("  Summarizing: "+strings.Join(args, ", ")))
	fmt.Println(tui.MutedStyle.Render("  Calling LLM — this may take 10-30 seconds…"))
	fmt.Println()
	result, err := summarize.SummarizePaths(args, cwd, cfg, opts)
	if err != nil {
		return fmt.Errorf("summarize failed: %w", err)
	}
	printSummarizeResult(result)
	return nil
}

func printSummarizeResult(result *summarize.Result) {
	fmt.Println(result.Content)
	if result.SavedTo != "" {
		fmt.Println()
		fmt.Printf("  %s  saved to %s\n",
			tui.SuccessStyle.Render("✓"),
			tui.PathStyle.Render(result.SavedTo),
		)
		fmt.Printf("  %s\n",
			tui.MutedStyle.Render("Tip: attach with --with file:"+result.SavedTo),
		)
		// Check if the file can be listed in the terminal pager.
		if _, err := os.Stat(result.SavedTo); err == nil {
			fmt.Printf("  %s\n",
				tui.MutedStyle.Render("View: cat "+result.SavedTo),
			)
		}
	}
}
