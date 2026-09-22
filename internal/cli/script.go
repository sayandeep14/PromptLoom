package cli

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/lmscr"
	"github.com/sayandeep14/PromptLoom/internal/tui"
)

var (
	scriptSets   []string
	scriptDir    string
	scriptDryRun bool
)

var scriptCmd = &cobra.Command{
	Use:   "script",
	Short: "Run a named sequence of loom commands (a .lmscr pipeline)",
}

var scriptRunCmd = &cobra.Command{
	Use:   "run <ScriptName>",
	Short: "Run every step of a loom script, in order",
	Long: `Runs each [[step]] of scripts/<Name>.lmscr (or --dir): each step names a loom (sub)command
and its arguments and is run exactly as if you had typed "loom <run> <args>..." yourself — a
script adds no capability those commands don't already have on their own; it only saves re-typing
a sequence of them, and lets one be checked in, reviewed and replayed.

A step's args may use {{vars.NAME}}, substituted from the script's own vars and --set (--set wins).
Every {{vars.NAME}} used anywhere in the script must have a value before anything runs.

By default a step only runs if every step before it succeeded ("when: on_success", the default);
"when: on_failure" runs a step only after an earlier one failed (for cleanup or notification);
"when: always" always runs it. A step with continue_on_fail = true does not stop the script, and
its failure does not count against the run, even though it is still reported.

Examples:
  loom script run Release
  loom script run Release --set env=production
  loom script run Release --dry-run           # show what would run; call nothing`,
	Args: cobra.ExactArgs(1),
	RunE: runScriptRun,
}

var scriptListCmd = &cobra.Command{
	Use:   "list",
	Short: "List loom script files",
	Long:  `Lists the scripts found in scripts/ (or --dir), with their step count and description.`,
	Args:  cobra.NoArgs,
	RunE:  runScriptList,
}

func init() {
	f := scriptRunCmd.Flags()
	f.StringArrayVar(&scriptSets, "set", nil, "set a script variable (key=value); overrides the script's own vars")
	f.StringVar(&scriptDir, "dir", "", "directory of scripts (default: scripts)")
	f.BoolVar(&scriptDryRun, "dry-run", false, "show each step's resolved command; call nothing")

	scriptListCmd.Flags().StringVar(&scriptDir, "dir", "", "directory of scripts (default: scripts)")

	scriptCmd.AddCommand(scriptRunCmd)
	scriptCmd.AddCommand(scriptListCmd)
}

func scriptsDir(cwd string) string {
	dir := scriptDir
	if dir == "" {
		dir = lmscr.DefaultDir
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	return dir
}

// scriptRunParams is everything runScriptRun needs, so it can be driven from tests without
// spawning a real loom subprocess for every step.
type scriptRunParams struct {
	Name   string
	Cwd    string
	Vars   map[string]string
	Dir    string
	DryRun bool

	Stdout io.Writer
	RunFn  func(ctx context.Context, cwd string, s *lmscr.Script, p lmscr.Params) (*lmscr.Outcome, error)
	Runner lmscr.Runner // passed through to lmscr.Params.Runner when RunFn is the default
	Binary string       // passed through to lmscr.Params.Binary
}

func runScriptRun(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	vars, err := tui.ParseKVArgs(scriptSets)
	if err != nil {
		return err
	}
	return executeScriptRun(scriptRunParams{
		Name: args[0], Cwd: cwd, Vars: vars, Dir: scriptDir, DryRun: scriptDryRun, Stdout: os.Stdout,
	})
}

func executeScriptRun(p scriptRunParams) error {
	if p.Stdout == nil {
		p.Stdout = os.Stdout
	}
	dir := p.Dir
	if dir == "" {
		dir = lmscr.DefaultDir
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(p.Cwd, dir)
	}
	path := filepath.Join(dir, p.Name+lmscr.SuiteSuffix)
	s, err := lmscr.LoadScript(path)
	if err != nil {
		return err
	}

	if p.DryRun {
		return printScriptDryRun(p.Stdout, s, p.Vars)
	}

	runFn := p.RunFn
	if runFn == nil {
		runFn = lmscr.Run
	}
	out, err := runFn(context.Background(), p.Cwd, s, lmscr.Params{Vars: p.Vars, Stdout: p.Stdout, Runner: p.Runner, Binary: p.Binary})
	if err != nil {
		return err
	}
	fmt.Fprint(p.Stdout, out.Text())
	if !out.OK() {
		return fmt.Errorf("the script did not complete cleanly")
	}
	return nil
}

func printScriptDryRun(w io.Writer, s *lmscr.Script, sets map[string]string) error {
	vars := map[string]string{}
	maps.Copy(vars, s.Vars)
	maps.Copy(vars, sets)
	fmt.Fprintf(w, "script: %s (%d step(s))\n", s.Name, len(s.Steps))
	if s.Description != "" {
		fmt.Fprintf(w, "  %s\n", s.Description)
	}
	for i, st := range s.Steps {
		fmt.Fprintf(w, "  %d. [%s] loom %s", i+1, st.Effective(), st.Run)
		for _, a := range st.Args {
			fmt.Fprintf(w, " %s", lmscr.Substitute(a, vars))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprint(w, "\n(dry run: nothing was sent)\n")
	return nil
}

func runScriptList(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	dir := scriptsDir(cwd)
	paths, err := lmscr.FindScripts(dir)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		fmt.Println("no scripts found")
		return nil
	}
	for _, p := range paths {
		s, err := lmscr.LoadScript(p)
		if err != nil {
			fmt.Printf("✗ %s: %v\n", p, err)
			continue
		}
		desc := s.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("%-20s %d step(s)  %s\n", s.Name, len(s.Steps), desc)
	}
	return nil
}
