package cli

import (
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/blame"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var blameCmd = &cobra.Command{
	Use:   "blame <Name>",
	Short: "Show git commit attribution for each field item in a prompt",
	Long: `Traces every resolved field item back to the git commit that last changed it.
Requires a git repository.

Examples:
  loom blame SecurityReviewer
  loom blame SecurityReviewer --field constraints
  loom blame SecurityReviewer --instruction "Check for hardcoded secrets"
  loom blame SecurityReviewer --since 2026-01-01
  loom blame SecurityReviewer --since HEAD~10`,
	Args: cobra.ExactArgs(1),
	RunE: runBlame,
}

var (
	blameFieldFlag       string
	blameSinceFlag       string
	blameInstructionFlag string
)

func init() {
	blameCmd.Flags().StringVar(&blameFieldFlag, "field", "", "limit to one field (e.g. constraints)")
	blameCmd.Flags().StringVar(&blameSinceFlag, "since", "", "only show items changed after this date or ref")
	blameCmd.Flags().StringVar(&blameInstructionFlag, "instruction", "", "filter to items containing this text")
}

func runBlame(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	promptName := args[0]

	var results []blame.Result
	_, err = tui.RunWithSpinner(fmt.Sprintf("blaming %s…", promptName), func() (string, error) {
		results, err = blame.RunBlame(promptName, blameFieldFlag, blameSinceFlag, blameInstructionFlag, cwd)
		return "", err
	})
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println(tui.MutedStyle.Render("  no attributed field items found"))
		return nil
	}

	sep := tui.MutedStyle.Render(strings.Repeat("─", 50))
	fmt.Println()

	for _, res := range results {
		header := tui.PromptNameStyle.Render(res.PromptName) +
			tui.MutedStyle.Render(" — ") +
			tui.HeaderStyle.Render(res.Field)
		fmt.Println("  " + header)
		fmt.Println()

		for _, item := range res.Items {
			bullet := tui.BulletStyle.Render("●")
			val := tui.TextStyle.Render(fmt.Sprintf("%q", truncateStr(item.Value, 80)))
			fmt.Printf("  %s  %s\n", bullet, val)

			fileStr := tui.PathStyle.Render(item.File)
			lineStr := ""
			if item.Line > 0 {
				lineStr = tui.MutedStyle.Render(fmt.Sprintf(" line %d", item.Line))
			}
			fmt.Printf("       %s %s%s\n",
				tui.MutedStyle.Render("from:"),
				fileStr,
				lineStr,
			)
			fmt.Printf("       %s %s\n",
				tui.MutedStyle.Render("origin:"),
				tui.MutedStyle.Render(item.Origin),
			)

			if item.Untracked {
				fmt.Printf("       %s\n", tui.MutedStyle.Render("commit: (untracked)"))
			} else if item.Commit.Hash != "" {
				date := item.Commit.Date.Format("2006-01-02")
				commitLine := fmt.Sprintf("%s  by %s  %s",
					tui.SuccessStyle.Render(item.Commit.Hash),
					tui.PromptNameStyle.Render(item.Commit.Author),
					tui.MutedStyle.Render(date),
				)
				fmt.Printf("       %s %s\n", tui.MutedStyle.Render("commit:"), commitLine)
				fmt.Printf("       %s %s\n",
					tui.MutedStyle.Render("msg:"),
					tui.MutedStyle.Render(fmt.Sprintf("%q", truncateStr(item.Commit.Summary, 72))),
				)
			}
			fmt.Println()
		}
		fmt.Println("  " + sep)
	}
	return nil
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
