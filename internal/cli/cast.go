package cli

import (
	"fmt"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var castSets []string
var castSlots []string
var castVarsFile string
var castProfile string
var castVariant string
var castOverlays []string
var castFormat string
var castWith []string
var castContext string
var castTo string

var castCmd = &cobra.Command{
	Use:   "cast <PromptName>",
	Short: "Render a prompt and send it to a named destination",
	Long: `Render a prompt and send the result to a named destination.

Examples:
  loom cast SecurityReviewer --to clipboard   # same as loom copy
  loom cast SecurityReviewer --to stdout
  loom cast SecurityReviewer --to file        # writes to dist/ (same as loom weave)
  loom cast SecurityReviewer --with git:diff --to clipboard`,
	Args: cobra.ExactArgs(1),
	RunE: runCast,
}

func init() {
	castCmd.Flags().StringVar(&castTo, "to", "clipboard", "destination: clipboard, stdout, file")
	castCmd.Flags().StringArrayVar(&castSets, "set", nil, "set a render variable (key=value)")
	castCmd.Flags().StringArrayVar(&castSlots, "slot", nil, "set a slot value (alias for --set)")
	castCmd.Flags().StringVar(&castVarsFile, "vars", "", "load render variables from a TOML file")
	castCmd.Flags().StringVar(&castProfile, "profile", "", "load a named profile from loom.toml")
	castCmd.Flags().StringVar(&castFormat, "format", "", "render format: markdown, json-anthropic, json-openai, cursor-rule, copilot, claude-code, plain")
	castCmd.Flags().StringVar(&castVariant, "variant", "", "apply a named prompt variant")
	castCmd.Flags().StringArrayVar(&castOverlays, "overlay", nil, "apply one or more overlays by name")
	castCmd.Flags().StringArrayVar(&castWith, "with", nil, "attach context: file:path, dir:path, git:diff, git:staged, stdin")
	castCmd.Flags().StringVar(&castContext, "context", "", "load a named context bundle from contexts/<name>.context")
}

func runCast(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	varsFile := castVarsFile
	if varsFile != "" && !filepath.IsAbs(varsFile) {
		varsFile = filepath.Join(cwd, varsFile)
	}
	fileVars, err := tui.LoadVarsFile(varsFile)
	if err != nil {
		return err
	}
	flagVars, err := tui.ParseKVArgs(append(append([]string{}, castSets...), castSlots...))
	if err != nil {
		return err
	}
	for k, v := range flagVars {
		fileVars[k] = v
	}

	opts := tui.CopyOptions{
		WeaveOptions: tui.WeaveOptions{
			Format:           castFormat,
			Variables:        fileVars,
			Profile:          castProfile,
			Variant:          castVariant,
			Overlays:         castOverlays,
			WithSources:      castWith,
			ContextBundle:    castContext,
			InteractiveSlots: castTo != "file",
		},
		Destination: castTo,
	}

	// stdout destination: skip spinner so output isn't polluted.
	if castTo == "stdout" {
		out, err := tui.RunCopy(name, opts, cwd)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}

	out, err := tui.RunWithSpinner("casting "+name+"…", func() (string, error) {
		return tui.RunCopy(name, opts, cwd)
	})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
