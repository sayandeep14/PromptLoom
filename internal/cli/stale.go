package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/loader"
	istale "github.com/sayandeepgiri/promptloom/internal/stale"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var staleCmd = &cobra.Command{
	Use:   "stale",
	Short: "Detect version mentions in prompts that don't match dependency files",
	Long: `Scan go.mod, package.json, pom.xml, Cargo.toml, pyproject.toml, and
requirements.txt for version declarations, then flag any prompt text that
mentions a different version of the same dependency.

Examples:
  loom stale                  # check all prompts
  loom stale CodeReviewer     # check a single prompt`,
	RunE: runStale,
}

func init() {
	rootCmd.AddCommand(staleCmd)
}

func runStale(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return err
	}

	// Check if any dependency files exist.
	deps, err := istale.ScanDeps(cwd)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom stale"))
	fmt.Println()

	if len(deps) == 0 {
		fmt.Println("  " + tui.MutedStyle.Render("No dependency files found (go.mod, package.json, pom.xml, etc.)."))
		fmt.Println()
		return nil
	}

	// Print detected deps summary.
	seen := map[string]bool{}
	for _, d := range deps {
		if !seen[d.Source] {
			fmt.Printf("  %s  (%d dependencies)\n",
				tui.MutedStyle.Render(d.Source),
				countSource(deps, d.Source))
			seen[d.Source] = true
		}
	}
	fmt.Println()

	// Resolve prompts.
	var prompts []*ast.ResolvedPrompt
	promptFilter := ""
	if len(args) > 0 {
		promptFilter = args[0]
	}
	for _, node := range reg.Prompts() {
		if promptFilter != "" && node.Name != promptFilter {
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
		return err
	}

	if len(findings) == 0 {
		fmt.Println("  " + tui.SuccessStyle.Render("All version mentions are current."))
		fmt.Println()
		return nil
	}

	for _, f := range findings {
		icon := tui.WarningStyle.Render("[stale]")
		fmt.Printf("  %s  %s.%s\n", icon, tui.PromptNameStyle.Render(f.Prompt), tui.MutedStyle.Render(f.Field))
		fmt.Printf("    %s declares %s  %s  but prompt mentions %s\n\n",
			tui.MutedStyle.Render(f.Dep),
			tui.SuccessStyle.Render(f.Version),
			tui.MutedStyle.Render("→"),
			tui.WarningStyle.Render(f.Mention))
	}

	fmt.Printf("  %s stale version mentions found.\n\n",
		tui.WarningStyle.Render(fmt.Sprintf("%d", len(findings))))
	return nil
}

func countSource(deps []istale.DepVersion, source string) int {
	n := 0
	for _, d := range deps {
		if d.Source == source {
			n++
		}
	}
	return n
}
