package cli

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

// Shell completion knows about the project: `loom weave <TAB>` offers the prompts in the
// current project, `--overlay <TAB>` its overlays, `--variant <TAB>` the variants of the prompt
// already typed, and `--format <TAB>` the render formats. (The scripts themselves come from
// `loom completion bash|zsh|fish|powershell`.) Completion is best effort: outside a project, or
// when the library does not load, it simply offers nothing.

var renderFormats = []string{"markdown", "json-anthropic", "json-openai", "cursor-rule", "copilot", "claude-code", "plain"}

func projectRegistry() *registry.Registry {
	cwd, err := resolveProjectDir()
	if err != nil {
		return nil
	}
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return nil
	}
	return reg
}

func withPrefix(candidates []string, prefix string) []string {
	var out []string
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(strings.SplitN(c, "\t", 2)[0]), strings.ToLower(prefix)) {
			out = append(out, c)
		}
	}
	return out
}

// promptCandidates lists prompts (and, when blocks is set, blocks) as "name<TAB>description".
func promptCandidates(reg *registry.Registry, blocks bool) []string {
	var out []string
	for _, n := range reg.Prompts() {
		desc := "prompt"
		if len(n.Parents) > 0 {
			desc = "prompt · inherits " + strings.Join(n.Parents, ", ")
		}
		out = append(out, n.Name+"\t"+desc)
	}
	if blocks {
		for _, n := range reg.Blocks() {
			out = append(out, n.Name+"\tblock")
		}
	}
	sort.Strings(out)
	return out
}

// completeNames completes the first positional argument (or the first two, when several is set,
// as for `loom diff A B`).
func completeNames(blocks bool, maxArgs int) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= maxArgs {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		reg := projectRegistry()
		if reg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(promptCandidates(reg, blocks), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

func completeFormats(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return withPrefix(renderFormats, toComplete), cobra.ShellCompDirectiveNoFileComp
}

func completeOverlays(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	reg := projectRegistry()
	if reg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, n := range reg.Overlays() {
		names = append(names, n.Name)
	}
	return withPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}

// completeVariantsOrEnvs offers the variant (or env) names of the prompt already typed, or of
// every prompt when none is typed yet.
func completeVariantsOrEnvs(envs bool) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		reg := projectRegistry()
		if reg == nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		seen := map[string]bool{}
		var names []string
		add := func(n *ast.Node) {
			if envs {
				for _, e := range n.EnvBlocks {
					if !seen[e.Name] {
						seen[e.Name] = true
						names = append(names, e.Name)
					}
				}
				return
			}
			for _, v := range n.Variants {
				if !seen[v.Name] {
					seen[v.Name] = true
					names = append(names, v.Name)
				}
			}
		}
		if len(args) > 0 {
			if n, ok := reg.LookupPrompt(args[0]); ok {
				add(n)
			}
		} else {
			for _, n := range reg.Prompts() {
				add(n)
			}
		}
		sort.Strings(names)
		return withPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// registerCompletions attaches the completions. It runs when the command tree is complete (all
// flags exist), right before the command line is executed.
func registerCompletions() {
	one := completeNames(false, 1)
	for _, c := range []*cobra.Command{weaveCmd, castCmd, copyCmd, traceCmd, unravelCmd, contractCmd, statsCmd,
		fingerprintCmd, auditCmd, doctorCmd, smellsCmd, testCmd, blameCmd, changelogCmd, runCmd} {
		if c.ValidArgsFunction == nil {
			c.ValidArgsFunction = one
		}
	}
	diffCmd.ValidArgsFunction = completeNames(false, 2)
	checkOutputCmd.ValidArgsFunction = completeNames(false, 1)
	graphCmd.ValidArgsFunction = completeNames(true, 1)
	impactCmd.ValidArgsFunction = completeNames(true, 1)

	for _, c := range []*cobra.Command{weaveCmd, castCmd, copyCmd, runCmd} {
		if c != runCmd {
			_ = c.RegisterFlagCompletionFunc("format", completeFormats)
		}
		_ = c.RegisterFlagCompletionFunc("overlay", completeOverlays)
		_ = c.RegisterFlagCompletionFunc("variant", completeVariantsOrEnvs(false))
	}
	_ = weaveCmd.RegisterFlagCompletionFunc("env", completeVariantsOrEnvs(true))
	_ = runCmd.RegisterFlagCompletionFunc("env", completeVariantsOrEnvs(true))
}
