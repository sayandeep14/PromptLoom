package cli

import (
	"fmt"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var copySets []string
var copySlots []string
var copyVarsFile string
var copyProfile string
var copyVariant string
var copyOverlays []string
var copyFormat string
var copyWith []string
var copyContext string

var copyCmd = &cobra.Command{
	Use:   "copy <PromptName>",
	Short: "Render a prompt and copy it to the clipboard",
	Long: `Render a prompt and copy the result directly to the clipboard.

Examples:
  loom copy SecurityReviewer
  loom copy SecurityReviewer --variant concise
  loom copy SecurityReviewer --with git:diff
  loom copy SecurityReviewer --context SpringService --with git:staged
  loom copy SecurityReviewer --profile local
  git diff | loom copy CodeReviewer --with stdin`,
	Args: cobra.ExactArgs(1),
	RunE: runCopy,
}

func init() {
	copyCmd.Flags().StringArrayVar(&copySets, "set", nil, "set a render variable (key=value)")
	copyCmd.Flags().StringArrayVar(&copySlots, "slot", nil, "set a slot value (alias for --set)")
	copyCmd.Flags().StringVar(&copyVarsFile, "vars", "", "load render variables from a TOML file")
	copyCmd.Flags().StringVar(&copyProfile, "profile", "", "load a named profile from loom.toml")
	copyCmd.Flags().StringVar(&copyFormat, "format", "", "render format: markdown, json-anthropic, json-openai, cursor-rule, copilot, claude-code, plain")
	copyCmd.Flags().StringVar(&copyVariant, "variant", "", "apply a named prompt variant")
	copyCmd.Flags().StringArrayVar(&copyOverlays, "overlay", nil, "apply one or more overlays by name")
	copyCmd.Flags().StringArrayVar(&copyWith, "with", nil, "attach context: file:path, dir:path, git:diff, git:staged, stdin")
	copyCmd.Flags().StringVar(&copyContext, "context", "", "load a named context bundle from contexts/<name>.context")
}

func runCopy(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	varsFile := copyVarsFile
	if varsFile != "" && !filepath.IsAbs(varsFile) {
		varsFile = filepath.Join(cwd, varsFile)
	}
	fileVars, err := tui.LoadVarsFile(varsFile)
	if err != nil {
		return err
	}
	flagVars, err := tui.ParseKVArgs(append(append([]string{}, copySets...), copySlots...))
	if err != nil {
		return err
	}
	for k, v := range flagVars {
		fileVars[k] = v
	}

	opts := tui.CopyOptions{
		WeaveOptions: tui.WeaveOptions{
			Format:           copyFormat,
			Variables:        fileVars,
			Profile:          copyProfile,
			Variant:          copyVariant,
			Overlays:         copyOverlays,
			WithSources:      copyWith,
			ContextBundle:    copyContext,
			InteractiveSlots: true,
		},
		Destination: "clipboard",
	}

	out, err := tui.RunWithSpinner("copying "+name+"…", func() (string, error) {
		return tui.RunCopy(name, opts, cwd)
	})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
