package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var todosCmd = &cobra.Command{
	Use:   "todos",
	Short: "List all todo: items across prompts and blocks",
	Long: `Print every item in a todo: field across the entire library.

Examples:
  loom todos                   # all prompts and blocks
  loom todos CodeReviewer      # single prompt`,
	RunE: runTodos,
}

func init() {
	rootCmd.AddCommand(todosCmd)
}

func runTodos(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return err
	}

	filter := ""
	if len(args) > 0 {
		filter = args[0]
	}

	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom todos"))
	fmt.Println()

	total := 0

	for _, node := range reg.Prompts() {
		if filter != "" && node.Name != filter {
			continue
		}
		todos := fieldValues(node.Fields, "todo")
		if len(todos) == 0 {
			continue
		}
		fmt.Println("  " + tui.PromptNameStyle.Render(node.Name) + tui.MutedStyle.Render("  (prompt)"))
		for _, item := range todos {
			fmt.Printf("    %s %s\n", tui.MutedStyle.Render("▸"), item)
			total++
		}
		fmt.Println()
	}

	for _, node := range reg.Blocks() {
		if filter != "" && node.Name != filter {
			continue
		}
		todos := fieldValues(node.Fields, "todo")
		if len(todos) == 0 {
			continue
		}
		fmt.Println("  " + tui.PromptNameStyle.Render(node.Name) + tui.MutedStyle.Render("  (block)"))
		for _, item := range todos {
			fmt.Printf("    %s %s\n", tui.MutedStyle.Render("▸"), item)
			total++
		}
		fmt.Println()
	}

	if total == 0 {
		fmt.Println("  " + tui.MutedStyle.Render("No todo items found."))
		fmt.Println()
		return nil
	}

	fmt.Printf("  %s todo items total.\n\n",
		tui.PromptNameStyle.Render(fmt.Sprintf("%d", total)))
	return nil
}
