package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/usage"
)

var (
	usageSince   string
	usageCommand string
	usageModel   string
	usageJSON    bool
	usageClear   bool
)

var usageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Report token and cost history from the project's usage ledger",
	Long: `Every call loom's commands make to a model — 'loom run', 'quest run', 'eval', 'score',
'optimize' and 'bench' — is recorded to <project>/.loom/usage.jsonl: when, which command, which
model, and how many tokens. A cost is estimated only for a model priced in loom.toml's [[pricing]];
loom never guesses a price, so an unpriced model shows token counts only.

Recording is best effort: a problem writing the ledger never fails the command that triggered it.

Examples:
  loom usage
  loom usage --since 2026-09-01
  loom usage --command eval --model gemini-2.5-flash
  loom usage --json
  loom usage --clear                    # start the ledger over`,
	Args: cobra.NoArgs,
	RunE: runUsage,
}

func init() {
	usageCmd.Flags().StringVar(&usageSince, "since", "", "only calls on or after this date (YYYY-MM-DD)")
	usageCmd.Flags().StringVar(&usageCommand, "command", "", "only this command (e.g. run, eval, optimize, bench)")
	usageCmd.Flags().StringVar(&usageModel, "model", "", "only this model: model or provider:model")
	usageCmd.Flags().BoolVar(&usageJSON, "json", false, "print JSON instead of a summary")
	usageCmd.Flags().BoolVar(&usageClear, "clear", false, "delete the ledger and start over; prints nothing else")
}

func runUsage(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	path := usage.DefaultPath(cwd)

	if usageClear {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	recs, err := usage.ReadAll(path)
	if err != nil {
		return err
	}
	f := usage.Filter{Command: usageCommand, Model: usageModel}
	if usageSince != "" {
		t, err := time.Parse("2006-01-02", usageSince)
		if err != nil {
			return fmt.Errorf("--since %q: use YYYY-MM-DD", usageSince)
		}
		f.Since = t
	}
	recs = f.Apply(recs)
	s := usage.Summarize(recs)

	if usageJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	fmt.Print(s.Text())
	return nil
}
