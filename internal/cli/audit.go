package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sayandeepgiri/promptloom/internal/audit"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var auditCmd = &cobra.Command{
	Use:   "audit [Name]",
	Short: "Scan prompts for dangerous instructions and security risks",
	Long: `Scans resolved prompt fields for dangerous patterns: hardcoded secret
references, policy-bypass instructions, destructive commands without
confirmation, production credential references, and more.

Examples:
  loom audit
  loom audit DeployAssistant
  loom audit --all`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAudit,
}

var auditAllFlag bool

func init() {
	auditCmd.Flags().BoolVar(&auditAllFlag, "all", false, "audit all prompts")
}

type auditResult struct {
	name     string
	findings []audit.Finding
	err      error
}

func runAudit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !auditAllFlag {
		auditAllFlag = true // default to --all when no name given
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}
	_ = cfg

	var results []auditResult
	_, _ = tui.RunWithSpinner("auditing prompts…", func() (string, error) {
		if !auditAllFlag {
			rp, err := resolve.Resolve(args[0], reg)
			if err != nil {
				results = append(results, auditResult{name: args[0], err: err})
				return "", nil
			}
			results = append(results, auditResult{
				name:     args[0],
				findings: audit.Audit(rp),
			})
			return "", nil
		}
		for _, node := range reg.Prompts() {
			rp, err := resolve.Resolve(node.Name, reg)
			if err != nil {
				results = append(results, auditResult{name: node.Name, err: err})
				continue
			}
			results = append(results, auditResult{
				name:     node.Name,
				findings: audit.Audit(rp),
			})
		}
		return "", nil
	})

	printAuditResults(results)

	// Exit codes: 0 = clean, 1 = high findings, 2 = medium only
	for _, r := range results {
		if audit.HasHigh(r.findings) {
			os.Exit(1)
		}
	}
	for _, r := range results {
		if audit.HasMedium(r.findings) {
			os.Exit(2)
		}
	}
	return nil
}

func printAuditResults(results []auditResult) {
	sep := tui.MutedStyle.Render(strings.Repeat("─", 57))
	fmt.Println()
	fmt.Println("  " + tui.HeaderStyle.Render("loom audit"))
	fmt.Println()
	fmt.Println("  " + sep)
	fmt.Println()

	allClean := true
	for _, r := range results {
		if r.err != nil {
			allClean = false
			nameStr := tui.ErrorStyle.Render(fmt.Sprintf("%-40s", r.name))
			fmt.Printf("  %s  ERROR\n", nameStr)
			fmt.Printf("  %s\n\n", tui.MutedStyle.Render("  "+r.err.Error()))
			continue
		}

		if len(r.findings) == 0 {
			nameStr := tui.SuccessStyle.Render(fmt.Sprintf("%-40s", r.name))
			fmt.Printf("  %s  PASS\n", nameStr)
			continue
		}

		allClean = false
		nameStr := tui.ErrorStyle.Render(fmt.Sprintf("%-40s", r.name))
		fmt.Printf("  %s  FAIL\n", nameStr)
		fmt.Println("  " + tui.MutedStyle.Render(strings.Repeat("─", 57)))

		for _, f := range r.findings {
			riskStr := riskStyle(f.Risk).Render(fmt.Sprintf("[%s]", f.Risk.String()))
			fieldStr := tui.MutedStyle.Render(f.Field + ":")
			valStr := fmt.Sprintf("%q", truncateStr(f.Value, 60))
			fmt.Printf("  %s   %s %s\n", riskStr, fieldStr, tui.TextStyle.Render(valStr))
			fmt.Printf("           %s %s\n", tui.MutedStyle.Render("Reason:"), f.Reason)
			fmt.Printf("           %s %s\n", tui.MutedStyle.Render("Fix:"), f.Fix)
			fmt.Println()
		}
	}

	fmt.Println("  " + sep)
	if allClean {
		fmt.Println("  " + tui.SuccessStyle.Render("All prompts passed audit."))
	}
	fmt.Println()
}

func riskStyle(r audit.RiskLevel) lipgloss.Style {
	switch r {
	case audit.High:
		return tui.ErrorStyle
	case audit.Medium:
		return tui.WarningStyle
	default:
		return tui.MutedStyle
	}
}
