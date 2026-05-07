package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/minimize"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var minimizeApply bool
var minimizeThreshold float64

var minimizeCmd = &cobra.Command{
	Use:   "minimize",
	Short: "Detect and remove redundant content from prompt libraries",
	Long: `Analyze prompts for exact duplicates, near-duplicates, and contradictions.

Without --apply the command is read-only and prints a report.
With --apply exact and near-duplicates are removed in-place from the resolved
output — source .loom files are not modified.

Examples:
  loom minimize                    # report only
  loom minimize --threshold 0.9    # stricter near-duplicate threshold
  loom minimize --apply            # apply deduplication to weave output`,
	RunE: runMinimize,
}

func init() {
	minimizeCmd.Flags().BoolVar(&minimizeApply, "apply", false, "remove duplicates from dist output (does not modify source files)")
	minimizeCmd.Flags().Float64Var(&minimizeThreshold, "threshold", 0.85, "similarity threshold for near-duplicate detection (0–1)")
	rootCmd.AddCommand(minimizeCmd)
}

func runMinimize(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return err
	}

	opts := minimize.Options{NearDupThreshold: minimizeThreshold}

	var allIssues []minimize.Issue
	var promptFilter string
	if len(args) > 0 {
		promptFilter = args[0]
	}

	for _, node := range reg.Prompts() {
		if promptFilter != "" && node.Name != promptFilter {
			continue
		}
		rp, err := resolve.Resolve(node.Name, reg)
		if err != nil {
			fmt.Printf("  %s  (skipped: %v)\n", tui.MutedStyle.Render(node.Name), err)
			continue
		}
		issues := minimize.Analyze(rp, opts)
		if minimizeApply {
			minimize.Apply(rp, opts)
		}
		allIssues = append(allIssues, issues...)
	}

	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom minimize"))
	fmt.Println()

	if len(allIssues) == 0 {
		fmt.Println("  " + tui.SuccessStyle.Render("No redundancy found."))
		fmt.Println()
		return nil
	}

	exact, near, contradictions := 0, 0, 0
	for _, iss := range allIssues {
		switch iss.Kind {
		case minimize.KindExactDuplicate:
			exact++
			icon := tui.ErrorStyle.Render("[exact-dup]")
			fmt.Printf("  %s  %s.%s\n", icon, tui.PromptNameStyle.Render(iss.Prompt), tui.MutedStyle.Render(iss.Field))
			fmt.Printf("    %s\n\n", tui.MutedStyle.Render(truncate(iss.ItemA, 80)))
		case minimize.KindNearDuplicate:
			near++
			icon := tui.WarningStyle.Render("[near-dup] ")
			fmt.Printf("  %s  %s.%s  (%.0f%% similar)\n", icon, tui.PromptNameStyle.Render(iss.Prompt), tui.MutedStyle.Render(iss.Field), iss.Similarity*100)
			fmt.Printf("    A: %s\n", tui.MutedStyle.Render(truncate(iss.ItemA, 80)))
			fmt.Printf("    B: %s\n\n", tui.MutedStyle.Render(truncate(iss.ItemB, 80)))
		case minimize.KindContradiction:
			contradictions++
			icon := tui.ErrorStyle.Render("[contradict]")
			fmt.Printf("  %s  %s.%s\n", icon, tui.PromptNameStyle.Render(iss.Prompt), tui.MutedStyle.Render(iss.Field))
			fmt.Printf("    A: %s\n", tui.MutedStyle.Render(truncate(iss.ItemA, 80)))
			fmt.Printf("    B: %s\n\n", tui.MutedStyle.Render(truncate(iss.ItemB, 80)))
		}
	}

	fmt.Printf("  %s exact duplicates  %s near-duplicates  %s contradictions\n\n",
		tui.ErrorStyle.Render(fmt.Sprintf("%d", exact)),
		tui.WarningStyle.Render(fmt.Sprintf("%d", near)),
		tui.ErrorStyle.Render(fmt.Sprintf("%d", contradictions)))

	if !minimizeApply && (exact+near) > 0 {
		fmt.Println("  " + tui.MutedStyle.Render("Run with --apply to remove exact and near-duplicates from output."))
		fmt.Println()
	}

	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
