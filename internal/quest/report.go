package quest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Text renders the whole run for a terminal.
func (o *Outcome) Text() string {
	var b strings.Builder
	if o.Quest.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", o.Quest.Description)
	}
	for i, s := range o.Steps {
		fmt.Fprintf(&b, "── step %d: %s (%s, %s) ──\n", i+1, s.Step.Name, s.Step.Prompt, s.Model)
		switch {
		case s.Err != nil:
			fmt.Fprintf(&b, "✗ %v\n", s.Err)
		default:
			fmt.Fprintln(&b, strings.TrimRight(s.Output, "\n"))
			for _, f := range s.ContractFailures {
				fmt.Fprintf(&b, "  ✗ contract: %s\n", f.Detail)
			}
		}
		b.WriteByte('\n')
	}
	total := len(o.Quest.Steps)
	ran := len(o.Steps)
	switch {
	case o.OK():
		fmt.Fprintf(&b, "%d/%d step(s) completed\n", ran, total)
	case o.Stopped && ran < total:
		fmt.Fprintf(&b, "stopped after %d/%d step(s) — pass --continue-on-error to run the rest anyway\n", ran, total)
	default:
		fmt.Fprintf(&b, "%d/%d step(s) completed, with problems\n", ran, total)
	}
	return b.String()
}

// Transcript writes a Markdown record of the run: every step's prompt, input and answer.
func (o *Outcome) Transcript(path string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# loom quest: %s\n", o.Quest.Name)
	if o.Quest.Description != "" {
		fmt.Fprintf(&b, "\n%s\n", o.Quest.Description)
	}
	for i, s := range o.Steps {
		fmt.Fprintf(&b, "\n## Step %d: %s\n\n- prompt: %s\n- model: %s\n\n### Input\n\n%s\n",
			i+1, s.Step.Name, s.Step.Prompt, s.Model, strings.TrimRight(s.Input, "\n"))
		if s.Err != nil {
			fmt.Fprintf(&b, "\n### Error\n\n%v\n", s.Err)
			continue
		}
		fmt.Fprintf(&b, "\n### Answer\n\n%s\n", strings.TrimRight(s.Output, "\n"))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
