package cli

import (
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/recipe"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var recipeCmd = &cobra.Command{
	Use:   "recipe",
	Short: "Scaffold prompt libraries from built-in templates",
	Long:  `Commands for listing and applying built-in prompt library recipes.`,
}

var recipeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available built-in recipes",
	RunE: func(cmd *cobra.Command, args []string) error {
		recipes := recipe.List()
		fmt.Println()
		fmt.Println("  " + tui.HeaderStyle.Render("Built-in Recipes"))
		fmt.Println()
		for _, r := range recipes {
			fmt.Printf("  %s\n", tui.PromptNameStyle.Render(fmt.Sprintf("%-22s", r.Name)))
			fmt.Printf("    %s\n", tui.MutedStyle.Render(r.Description))
			if len(r.Flags) > 0 {
				fmt.Printf("    %s %s\n", tui.MutedStyle.Render("flags:"), tui.ArgDescStyle.Render(strings.Join(r.Flags, "  ")))
			}
			fmt.Println()
		}
		fmt.Println("  " + tui.MutedStyle.Render("Usage: loom recipe apply <name> [flags]"))
		fmt.Println()
		return nil
	},
}

var (
	recipeLanguage  string
	recipeFramework string
	recipeStyle     string
	recipeForce     bool
)

var recipeApplyCmd = &cobra.Command{
	Use:   "apply <recipe>",
	Short: "Apply a recipe to scaffold a prompt library",
	Long: `Scaffold a prompt library from a built-in recipe template.

Examples:
  loom recipe apply reviewer --language go --framework gin
  loom recipe apply reviewer --language rust --framework axum
  loom recipe apply reviewer --language java --framework spring-boot
  loom recipe apply api-designer --style graphql
  loom recipe apply migration-assistant
  loom recipe apply security-auditor
  loom recipe apply docs-writer`,
	Args: cobra.ExactArgs(1),
	RunE: runRecipeApply,
}

func init() {
	recipeApplyCmd.Flags().StringVar(&recipeLanguage, "language", "", "programming language (e.g. go, rust, java, typescript)")
	recipeApplyCmd.Flags().StringVar(&recipeFramework, "framework", "", "framework (e.g. gin, axum, spring-boot, react)")
	recipeApplyCmd.Flags().StringVar(&recipeStyle, "style", "rest", "API style for api-designer: rest or graphql")
	recipeApplyCmd.Flags().BoolVar(&recipeForce, "force", false, "overwrite existing files")
	recipeCmd.AddCommand(recipeListCmd)
	recipeCmd.AddCommand(recipeApplyCmd)
}

func runRecipeApply(cmd *cobra.Command, args []string) error {
	name := args[0]

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	opts := recipe.Options{
		Language:  recipeLanguage,
		Framework: recipeFramework,
		Style:     recipeStyle,
	}

	var result recipe.ApplyResult
	var applyErr error
	_, applyErr = tui.RunWithSpinner("applying recipe "+name+"…", func() (string, error) {
		result, applyErr = recipe.Apply(name, opts, cwd, recipeForce)
		return "", applyErr
	})
	if applyErr != nil {
		return applyErr
	}

	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom recipe apply  "+name))
	fmt.Println()

	for _, f := range result.Written {
		fmt.Println("  " + tui.SuccessStyle.Render("  wrote ") + tui.PathStyle.Render(f))
	}
	for _, f := range result.Skipped {
		fmt.Println("  " + tui.MutedStyle.Render("  skip  ") + tui.PathStyle.Render(f) + tui.MutedStyle.Render(" (exists)"))
	}

	fmt.Printf("\n  %s written, %s skipped\n\n",
		tui.SuccessStyle.Render(fmt.Sprintf("%d", len(result.Written))),
		tui.MutedStyle.Render(fmt.Sprintf("%d", len(result.Skipped))))

	if len(result.Written) > 0 {
		fmt.Println("  " + tui.MutedStyle.Render("Next:  loom inspect  →  loom weave --all"))
		fmt.Println()

		// Run inspect automatically.
		out, _ := tui.RunWithSpinner("running loom inspect…", func() (string, error) {
			o, _ := tui.RunInspect(cwd)
			return o, nil
		})
		fmt.Print(out)
	}
	return nil
}
