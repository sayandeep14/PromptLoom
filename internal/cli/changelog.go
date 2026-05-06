package cli

import (
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/blame"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var changelogCmd = &cobra.Command{
	Use:   "changelog [Name]",
	Short: "Show a prompt-centric change log from git history",
	Long: `Scans git history for changes to .loom source files and presents a
prompt-level view of what changed, when, and by whom.

Requires a git repository.

Examples:
  loom changelog
  loom changelog SecurityReviewer
  loom changelog --since HEAD~5
  loom changelog --since 2026-04-01
  loom changelog --format markdown`,
	Args: cobra.MaximumNArgs(1),
	RunE: runChangelog,
}

var (
	changelogSinceFlag  string
	changelogFormatFlag string
)

func init() {
	changelogCmd.Flags().StringVar(&changelogSinceFlag, "since", "", "limit to commits after this date (YYYY-MM-DD) or ref (HEAD~N)")
	changelogCmd.Flags().StringVar(&changelogFormatFlag, "format", "text", "output format: text or markdown")
}

func runChangelog(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	promptFilter := ""
	if len(args) == 1 {
		promptFilter = args[0]
	}

	var changelogs []blame.PromptChangelog
	_, err = tui.RunWithSpinner("building changelog…", func() (string, error) {
		changelogs, err = blame.BuildChangelog(cwd, changelogSinceFlag, promptFilter)
		return "", err
	})
	if err != nil {
		return err
	}

	if len(changelogs) == 0 {
		fmt.Println(tui.MutedStyle.Render("  no prompt changes found in the specified range"))
		return nil
	}

	if changelogFormatFlag == "markdown" {
		printChangelogMarkdown(changelogs)
	} else {
		printChangelogText(changelogs)
	}
	return nil
}

func printChangelogText(changelogs []blame.PromptChangelog) {
	sep := tui.MutedStyle.Render(strings.Repeat("─", 50))
	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("Prompt Changelog"))
	fmt.Println()

	for _, cl := range changelogs {
		fmt.Println("  " + tui.PromptNameStyle.Render(cl.Name))
		fmt.Println("  " + tui.MutedStyle.Render(strings.Repeat("─", len(cl.Name)+2)))

		for _, e := range cl.Entries {
			date := tui.MutedStyle.Render(e.Date.Format("2006-01-02"))
			msg := tui.TextStyle.Render(e.Message)
			fmt.Printf("  %s  %s\n", date, msg)
		}
		fmt.Println()
	}
	fmt.Println("  " + sep)
}

func printChangelogMarkdown(changelogs []blame.PromptChangelog) {
	fmt.Println("# Prompt Changelog")
	fmt.Println()

	for _, cl := range changelogs {
		fmt.Printf("## %s\n\n", cl.Name)
		for _, e := range cl.Entries {
			date := e.Date.Format("2006-01-02")
			fmt.Printf("- **%s** — %s\n", date, e.Message)
		}
		fmt.Println()
	}
}
