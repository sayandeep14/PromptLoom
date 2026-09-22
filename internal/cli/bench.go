package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/bench"
	"github.com/sayandeep14/PromptLoom/internal/tui"
)

var (
	benchInput     string
	benchInputFile string
	benchModels    string
	benchRuns      int
	benchJSON      bool
	benchSets      []string
)

var benchCmd = &cobra.Command{
	Use:   "bench <PromptName>",
	Short: "Time and price a prompt's answer across one or more models",
	Long: `Sends the same input to a prompt --runs times per model and reports latency and token usage,
plus an estimated cost for any model priced in loom.toml's [[pricing]] — not a quality judgement
(see 'loom eval' for that), only how long an answer took and what it cost, so a model choice can
weigh speed and price alongside eval's scores.

Every call is recorded to the project's usage ledger (see 'loom usage') under command "bench".

Examples:
  loom bench CodeReviewer --input-file diff.patch
  loom bench CodeReviewer --input "review this" --models gemini-2.5-flash,anthropic:claude-sonnet-4-6
  loom bench CodeReviewer --input "review this" --runs 5`,
	Args: cobra.ExactArgs(1),
	RunE: runBench,
}

func init() {
	benchCmd.Flags().StringVarP(&benchInput, "input", "i", "", "the message to send")
	benchCmd.Flags().StringVar(&benchInputFile, "input-file", "", "read the message from a file")
	benchCmd.Flags().StringVar(&benchModels, "models", "", "comma-separated models to time: model or provider:model (default: [testing] in loom.toml)")
	benchCmd.Flags().IntVar(&benchRuns, "runs", 1, "calls per model")
	benchCmd.Flags().BoolVar(&benchJSON, "json", false, "print JSON instead of a table")
	benchCmd.Flags().StringArrayVar(&benchSets, "set", nil, "set a render variable (key=value)")
}

func runBench(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	if benchInput != "" && benchInputFile != "" {
		return fmt.Errorf("use --input or --input-file, not both")
	}
	input := benchInput
	if benchInputFile != "" {
		data, err := os.ReadFile(absIn(cwd, benchInputFile))
		if err != nil {
			return fmt.Errorf("--input-file: %w", err)
		}
		input = string(data)
	}
	var models []string
	for _, m := range strings.Split(benchModels, ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	vars, err := tui.ParseKVArgs(benchSets)
	if err != nil {
		return err
	}

	out, err := bench.Run(context.Background(), cwd, args[0], bench.Params{Input: input, Variables: vars, Models: models, Runs: benchRuns})
	if err != nil {
		return err
	}
	if benchJSON {
		return printBenchJSON(out)
	}
	fmt.Print(out.Text())
	return nil
}

func printBenchJSON(out *bench.Outcome) error {
	type call struct {
		DurationMS int     `json:"duration_ms"`
		Input      int     `json:"input_tokens"`
		Output     int     `json:"output_tokens"`
		CostUSD    float64 `json:"cost_usd,omitempty"`
		HasCost    bool    `json:"has_cost"`
		Err        string  `json:"error,omitempty"`
	}
	type model struct {
		Label string `json:"model"`
		Calls []call `json:"calls"`
	}
	res := struct {
		Prompt string  `json:"prompt"`
		Models []model `json:"models"`
	}{Prompt: out.Prompt}
	for _, m := range out.Models {
		mm := model{Label: m.Label}
		for _, c := range m.Calls {
			cc := call{DurationMS: int(c.Duration.Milliseconds()), Input: c.Usage.InputTokens, Output: c.Usage.OutputTokens, HasCost: c.HasCost, CostUSD: c.CostUSD}
			if c.Err != nil {
				cc.Err = c.Err.Error()
			}
			mm.Calls = append(mm.Calls, cc)
		}
		res.Models = append(res.Models, mm)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}
