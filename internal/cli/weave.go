package cli

import (
	"fmt"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var weaveAll bool
var weaveOut string
var weaveStdout bool
var weaveSets []string
var weaveSlots []string
var weaveVarsFile string
var weaveProfile string
var weaveVariant string
var weaveOverlays []string
var weaveSourceMap bool
var weaveFormat string
var weaveWith []string
var weaveContext string
var weaveWatch bool
var weaveIncremental bool

var weaveCmd = &cobra.Command{
	Use:   "weave [PromptName]",
	Short: "Render prompt(s) to compiled prompt artifacts",
	Long: `Resolve and render one or all prompts to compiled prompt artifacts.

Examples:
  loom weave SecurityReviewer             # write to dist/prompts/SecurityReviewer.md
  loom weave SecurityReviewer --stdout    # print to stdout
  loom weave SecurityReviewer --out ./out.md
  loom weave SecurityReviewer --format json-anthropic
  loom weave CodeAssistant --format claude-code
  loom weave LanguageReviewer --profile local --set language=Go
  loom weave MigrationPlanner             # prompts for required slots
  loom weave CodeReviewer --variant concise --overlay security-focus
  loom weave SecurityReviewer --sourcemap # write Markdown + .loom.map.json
  loom weave CodeReviewer --with file:src/main.go
  loom weave BugFixer --with git:diff
  loom weave SecurityReviewer --context SpringService --with git:staged
  git diff | loom weave CodeReviewer --with stdin
  loom weave --all --profile frontend     # render every prompt`,
	RunE: runWeave,
}

func init() {
	weaveCmd.Flags().BoolVar(&weaveAll, "all", false, "render all prompts")
	weaveCmd.Flags().StringVar(&weaveOut, "out", "", "write output to this path (single-prompt only)")
	weaveCmd.Flags().BoolVar(&weaveStdout, "stdout", false, "print to stdout instead of writing a file")
	weaveCmd.Flags().StringArrayVar(&weaveSets, "set", nil, "set a render variable (key=value)")
	weaveCmd.Flags().StringArrayVar(&weaveSlots, "slot", nil, "set a slot value (alias for --set)")
	weaveCmd.Flags().StringVar(&weaveVarsFile, "vars", "", "load render variables from a TOML file")
	weaveCmd.Flags().StringVar(&weaveProfile, "profile", "", "load a named profile from loom.toml")
	weaveCmd.Flags().StringVar(&weaveFormat, "format", "", "render format: markdown, json-anthropic, json-openai, cursor-rule, copilot, claude-code, plain")
	weaveCmd.Flags().StringVar(&weaveVariant, "variant", "", "apply a named prompt variant")
	weaveCmd.Flags().StringArrayVar(&weaveOverlays, "overlay", nil, "apply one or more overlays by name")
	weaveCmd.Flags().BoolVar(&weaveSourceMap, "sourcemap", false, "write a .loom.map.json source map alongside rendered output")
	weaveCmd.Flags().StringArrayVar(&weaveWith, "with", nil, "attach context: file:path, dir:path, git:diff, git:staged, stdin")
	weaveCmd.Flags().StringVar(&weaveContext, "context", "", "load a named context bundle from contexts/<name>.context")
	weaveCmd.Flags().BoolVar(&weaveWatch, "watch", false, "re-render on every source file change (requires --all)")
	weaveCmd.Flags().BoolVar(&weaveIncremental, "incremental", false, "skip prompts whose resolved hash is unchanged (requires --all)")
}

func runWeave(cmd *cobra.Command, args []string) error {
	if !weaveAll && len(args) == 0 {
		return fmt.Errorf("specify a prompt name or use --all")
	}
	if weaveAll && len(args) > 0 {
		return fmt.Errorf("cannot combine a prompt name with --all")
	}
	if weaveWatch && !weaveAll {
		return fmt.Errorf("--watch requires --all")
	}
	if weaveIncremental && !weaveAll {
		return fmt.Errorf("--incremental requires --all")
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	varsFile := weaveVarsFile
	if varsFile != "" && !filepath.IsAbs(varsFile) {
		varsFile = filepath.Join(cwd, varsFile)
	}
	fileVars, err := tui.LoadVarsFile(varsFile)
	if err != nil {
		return err
	}
	flagVars, err := tui.ParseKVArgs(append(append([]string{}, weaveSets...), weaveSlots...))
	if err != nil {
		return err
	}
	for key, value := range flagVars {
		fileVars[key] = value
	}

	spinLabel := "weaving all prompts…"
	if !weaveAll {
		spinLabel = "weaving " + name + "…"
	}

	opts := tui.WeaveOptions{
		OutPath:          weaveOut,
		Stdout:           weaveStdout,
		Format:           weaveFormat,
		Variables:        fileVars,
		Profile:          weaveProfile,
		Variant:          weaveVariant,
		Overlays:         weaveOverlays,
		SourceMap:        weaveSourceMap,
		InteractiveSlots: !weaveAll,
		WithSources:      weaveWith,
		ContextBundle:    weaveContext,
		Incremental:      weaveIncremental,
	}

	// Watch mode: blocking loop, prints directly to stdout.
	if weaveWatch {
		return tui.WatchWeave(opts, cwd)
	}

	var out string
	if weaveStdout {
		// Don't wrap with spinner when printing directly to stdout.
		out, err = tui.RunWeave(name, weaveAll, opts, cwd)
	} else {
		out, err = tui.RunWithSpinner(spinLabel, func() (string, error) { //nolint:errcheck
			return tui.RunWeave(name, weaveAll, opts, cwd)
		})
	}
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
