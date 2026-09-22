package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/quest"
)

var (
	questInput           string
	questInputFile       string
	questModel           string
	questOut             string
	questJSON            bool
	questDryRun          bool
	questContinueOnError bool
	questNoStream        bool
	questDir             string
)

var questCmd = &cobra.Command{
	Use:   "quest",
	Short: "Run a named sequence of prompts (a small, fixed pipeline)",
}

var questRunCmd = &cobra.Command{
	Use:   "run <QuestName>",
	Short: "Run every step of a quest, in order",
	Long: `Runs each [[step]] of quests/<Name>.quest.toml (or --dir) as 'loom run' would: the prompt is
resolved and rendered, sent to a model, and the reply streamed to the terminal. A step's input may
use {{quest.input}} (this run's --input) and, after the first step, {{quest.previous}} (the
previous step's answer) — that is the only way one step's output reaches the next; nothing here
lets a step call a tool or act on your behalf. A step stops the quest on error or contract
violation unless the step sets continue_on_fail, or you pass --continue-on-error.

Examples:
  loom quest run Onboarding --input "new hire, backend team"
  loom quest run Triage --input-file ticket.txt --out transcript.md
  loom quest run Onboarding --dry-run --input "..."   # show the first step's prompt; call nothing`,
	Args: cobra.ExactArgs(1),
	RunE: runQuestRun,
}

var questListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quest files",
	Long:  `Lists the quests found in quests/ (or --dir), with their step count and description.`,
	Args:  cobra.NoArgs,
	RunE:  runQuestList,
}

func init() {
	f := questRunCmd.Flags()
	f.StringVarP(&questInput, "input", "i", "", "the quest's input ({{quest.input}} in step 1)")
	f.StringVar(&questInputFile, "input-file", "", "read the input from a file")
	f.StringVar(&questModel, "model", "", "model to use for every step: model or provider:model (default: [testing] in loom.toml)")
	f.StringVar(&questOut, "out", "", "write a Markdown transcript to this file (needs permission.write)")
	f.BoolVar(&questJSON, "json", false, "print one JSON object instead of the transcript text")
	f.BoolVar(&questDryRun, "dry-run", false, "show the first step's prompt and input; call nothing")
	f.BoolVar(&questContinueOnError, "continue-on-error", false, "run every step even if one errors or fails its contract")
	f.BoolVar(&questNoStream, "no-stream", false, "wait for each step's whole answer instead of streaming it")
	f.StringVar(&questDir, "dir", "", "directory of quest files (default: quests)")

	questListCmd.Flags().StringVar(&questDir, "dir", "", "directory of quest files (default: quests)")

	questCmd.AddCommand(questRunCmd)
	questCmd.AddCommand(questListCmd)
}

func questFile(cwd, name string) string {
	dir := questDir
	if dir == "" {
		dir = quest.DefaultDir
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	return filepath.Join(dir, name+quest.SuiteSuffix)
}

func runQuestRun(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	if questInput != "" && questInputFile != "" {
		return fmt.Errorf("use --input or --input-file, not both")
	}
	q, err := quest.LoadQuest(questFile(cwd, args[0]))
	if err != nil {
		return err
	}

	perm, err := agent.LoadPermission(cwd)
	if err != nil {
		return err
	}

	input := questInput
	if questInputFile != "" {
		path := absIn(cwd, questInputFile)
		if err := perm.CheckRead(path); err != nil {
			return fmt.Errorf("--input-file %s: %w", questInputFile, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("--input-file: %w", err)
		}
		input = string(data)
	}
	if questOut != "" {
		if err := perm.CheckWrite(absIn(cwd, questOut)); err != nil {
			return fmt.Errorf("--out %s: %w", questOut, err)
		}
	}

	if questDryRun {
		return printQuestDryRun(q, input)
	}

	ctx := context.Background()
	var san agent.Sanitizer
	stdoutTTY := isatty.IsTerminal(os.Stdout.Fd())
	params := quest.Params{
		Input: input, Model: questModel, ContinueOnError: questContinueOnError, Stream: !questNoStream,
	}
	if !questJSON {
		params.OnDelta = func(step int, d string) {
			if stdoutTTY {
				d = san.Filter(d)
			}
			fmt.Print(d)
		}
	}

	var lastStep = -1
	if !questJSON {
		wrapped := params.OnDelta
		params.OnDelta = func(step int, d string) {
			if step != lastStep {
				if lastStep >= 0 {
					fmt.Println()
				}
				s := q.Steps[step]
				fmt.Printf("── step %d: %s ──\n", step+1, s.Name)
				lastStep = step
			}
			wrapped(step, d)
		}
	}

	out, err := quest.Run(ctx, cwd, q, params, perm)
	if err != nil {
		return err
	}
	if !questJSON && len(out.Steps) > 0 {
		fmt.Println()
	}

	if questOut != "" && len(out.Steps) > 0 {
		if err := out.Transcript(absIn(cwd, questOut)); err != nil {
			return err
		}
	}

	if questJSON {
		if err := printQuestJSON(out); err != nil {
			return err
		}
	} else {
		for _, s := range out.Steps {
			printQuestFooter(s)
		}
		total := len(q.Steps)
		ran := len(out.Steps)
		if out.Stopped {
			fmt.Printf("  stopped after %d/%d step(s)\n", ran, total)
		}
	}

	if !out.OK() {
		return fmt.Errorf("the quest did not complete cleanly")
	}
	return nil
}

func printQuestFooter(s quest.StepResult) {
	if s.Err != nil {
		fmt.Printf("  ✗ %s: %v\n", s.Step.Name, s.Err)
		return
	}
	for _, f := range s.ContractFailures {
		fmt.Printf("  ✗ %s: contract: %s\n", s.Step.Name, f.Detail)
	}
}

func printQuestDryRun(q *quest.Quest, input string) error {
	first := q.Steps[0]
	fmt.Printf("quest: %s (%d step(s))\n", q.Name, len(q.Steps))
	for i, s := range q.Steps {
		fmt.Printf("  %d. %s → %s\n", i+1, s.Name, s.Prompt)
	}
	fmt.Printf("\n── step 1: %s (%s) ──\n\n%s\n", first.Name, first.Prompt, strings.ReplaceAll(first.Input, "{{quest.input}}", input))
	fmt.Print("\n(dry run: nothing was sent)\n")
	return nil
}

func printQuestJSON(out *quest.Outcome) error {
	steps := make([]map[string]any, 0, len(out.Steps))
	for _, s := range out.Steps {
		var errStr string
		if s.Err != nil {
			errStr = s.Err.Error()
		}
		var failures []string
		for _, f := range s.ContractFailures {
			failures = append(failures, f.Detail)
		}
		steps = append(steps, map[string]any{
			"step": s.Step.Name, "model": s.Model, "input": s.Input, "output": s.Output,
			"error": errStr, "contract_failures": append([]string{}, failures...),
			"duration_ms": s.Duration.Milliseconds(),
			"usage":       map[string]int{"input_tokens": s.Usage.InputTokens, "output_tokens": s.Usage.OutputTokens},
		})
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"quest": out.Quest.Name, "ok": out.OK(), "stopped": out.Stopped, "steps": steps})
}

func runQuestList(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	dir := questDir
	if dir == "" {
		dir = quest.DefaultDir
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	paths, err := quest.FindQuests(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Println("no quests found")
		return nil
	}
	for _, p := range paths {
		q, err := quest.LoadQuest(p)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", p, err)
			continue
		}
		desc := q.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("%-20s %d step(s)  %s\n", q.Name, len(q.Steps), desc)
	}
	return nil
}
