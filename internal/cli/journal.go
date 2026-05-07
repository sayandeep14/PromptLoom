package cli

import (
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/journal"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var journalCmd = &cobra.Command{
	Use:   "journal",
	Short: "Manage the prompt library change journal",
	Long:  `Commands for adding and listing journal entries stored in .loom/journal/.`,
}

var (
	journalPrompt string
	journalAuthor string
	journalBody   string
)

var journalAddCmd = &cobra.Command{
	Use:   "add <message>",
	Short: "Add a new journal entry",
	Long: `Create a new journal entry in .loom/journal/.

Examples:
  loom journal add "Refactored SecurityReviewer to inherit from BaseEngineer"
  loom journal add "Added Go conventions block" --prompt GoConventions --author alice`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := resolveProjectDir()
		if err != nil {
			return err
		}
		message := strings.Join(args, " ")
		path, err := journal.Add(cwd, message, journalPrompt, journalAuthor, journalBody)
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println("  " + tui.SuccessStyle.Render("Journal entry created"))
		fmt.Println("  " + tui.PathStyle.Render(path))
		fmt.Println()
		return nil
	},
}

var journalListCmd = &cobra.Command{
	Use:   "list [PromptName]",
	Short: "List journal entries, optionally filtered by prompt name",
	Long: `Print all journal entries, newest first.

Examples:
  loom journal list
  loom journal list CodeReviewer`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := resolveProjectDir()
		if err != nil {
			return err
		}

		var entries []journal.Entry
		if len(args) > 0 {
			entries, err = journal.ForPrompt(cwd, args[0])
		} else {
			entries, err = journal.List(cwd)
		}
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Println("  " + tui.HeaderStyle.Render("loom journal"))
		fmt.Println()

		if len(entries) == 0 {
			fmt.Println("  " + tui.MutedStyle.Render("No journal entries found. Use: loom journal add <message>"))
			fmt.Println()
			return nil
		}

		sep := tui.MutedStyle.Render(strings.Repeat("─", 60))
		for _, e := range entries {
			date := e.Date.Format("2006-01-02")
			dateStr := tui.MutedStyle.Render(date)
			msg := tui.PromptNameStyle.Render(e.Message)
			prompt := ""
			if e.Prompt != "" {
				prompt = "  " + tui.MutedStyle.Render("["+e.Prompt+"]")
			}
			author := ""
			if e.Author != "" {
				author = "  " + tui.MutedStyle.Render("by "+e.Author)
			}
			fmt.Printf("  %s  %s%s%s\n", dateStr, msg, prompt, author)
		}
		fmt.Printf("\n  %s\n", sep)
		fmt.Printf("  %s entries\n\n", tui.MutedStyle.Render(fmt.Sprintf("%d", len(entries))))
		return nil
	},
}

func init() {
	journalAddCmd.Flags().StringVar(&journalPrompt, "prompt", "", "associate entry with a prompt name")
	journalAddCmd.Flags().StringVar(&journalAuthor, "author", "", "author name for the entry")
	journalAddCmd.Flags().StringVar(&journalBody, "body", "", "additional markdown body text")

	journalCmd.AddCommand(journalAddCmd)
	journalCmd.AddCommand(journalListCmd)
	rootCmd.AddCommand(journalCmd)
}
