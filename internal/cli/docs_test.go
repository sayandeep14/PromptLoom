package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// These tests keep docs/LOOM_COMMAND.md and docs/LOOM_LANGUAGE.md honest: every command and
// flag the docs mention must exist, every command must be documented, and every DSL example
// must parse.

const docsDir = "../../docs"

func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(docsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// commandPaths returns every runnable command path below root ("pack build", "weave", ...).
func commandPaths(root *cobra.Command) map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	var walk func(c *cobra.Command, prefix string)
	walk = func(c *cobra.Command, prefix string) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			path := strings.TrimSpace(prefix + " " + sub.Name())
			out[path] = sub
			walk(sub, path)
		}
	}
	walk(root, "")
	return out
}

var headingRe = regexp.MustCompile("(?m)^#{2,4} `loom ([a-z][a-z -]*)")

// section splits a doc into `### \`loom X\“ sections keyed by the command path.
func commandSections(doc string) map[string]string {
	locs := headingRe.FindAllStringSubmatchIndex(doc, -1)
	out := map[string]string{}
	for i, l := range locs {
		end := len(doc)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		// stop at the next "## " section heading too
		body := doc[l[0]:end]
		if j := strings.Index(body[3:], "\n## "); j >= 0 {
			body = body[:j+3]
		}
		out[strings.TrimSpace(doc[l[2]:l[3]])] = body
	}
	return out
}

func TestEveryCommandIsDocumented(t *testing.T) {
	doc := readDoc(t, "LOOM_COMMAND.md")
	sections := commandSections(doc)
	for path, c := range commandPaths(rootCmd) {
		if c.HasSubCommands() && !c.Runnable() {
			continue // a pure group; its subcommands are checked individually
		}
		_, ok := sections[path]
		// a subcommand may be documented under its group ("loom thread block <Name>")
		if !ok && strings.Contains(doc, "`loom "+path) {
			ok = true
		}
		if !ok {
			t.Errorf("`loom %s` is not documented in docs/LOOM_COMMAND.md (add a ### `loom %s` section)", path, path)
		}
	}
}

func TestDocumentedCommandsExist(t *testing.T) {
	doc := readDoc(t, "LOOM_COMMAND.md")
	paths := commandPaths(rootCmd)
	for path := range commandSections(doc) {
		// headings may carry arguments: "pack install <path>"
		fields := strings.Fields(path)
		var words []string
		for _, f := range fields {
			words = append(words, f)
		}
		found := false
		for n := len(words); n > 0 && !found; n-- {
			_, found = paths[strings.Join(words[:n], " ")]
		}
		if !found {
			t.Errorf("docs/LOOM_COMMAND.md documents `loom %s`, which does not exist", path)
		}
	}
}

var flagRe = regexp.MustCompile("(?:^|[\\s`(\\[|])(--[a-z][a-z0-9-]+)")

func allFlags(c *cobra.Command) map[string]bool {
	out := map[string]bool{}
	add := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) { out[f.Name] = true })
	}
	add(c.Flags())
	add(c.InheritedFlags())
	add(c.PersistentFlags())
	// a group ("thread") is documented together with its subcommands
	for _, sub := range c.Commands() {
		for f := range allFlags(sub) {
			out[f] = true
		}
	}
	return out
}

var loomInvocationRe = regexp.MustCompile("loom ([a-z][a-z-]*)(?: ([a-z][a-z-]*))?")

// resolveCommand finds the command an invocation like "pack build" or "weave --all" refers to.
func resolveCommand(paths map[string]*cobra.Command, first, second string) *cobra.Command {
	if second != "" {
		if c, ok := paths[first+" "+second]; ok {
			return c
		}
	}
	return paths[first]
}

func TestDocumentedFlagsExist(t *testing.T) {
	paths := commandPaths(rootCmd)
	for _, name := range []string{"LOOM_COMMAND.md", "LOOM_LANGUAGE.md", "../README.md"} {
		doc := readDoc(t, name)
		sections := commandSections(doc)
		// the section a line belongs to, so a flag with no `loom <cmd>` before it is checked
		// against the command being described
		var current *cobra.Command
		for n, line := range strings.Split(doc, "\n") {
			if m := headingRe.FindStringSubmatch(line); m != nil {
				words := strings.Fields(m[1])
				current = nil
				for k := len(words); k > 0 && current == nil; k-- {
					current = paths[strings.Join(words[:k], " ")]
				}
			}
			_ = sections
			flags := flagRe.FindAllStringSubmatchIndex(line, -1)
			if len(flags) == 0 {
				continue
			}
			invocations := loomInvocationRe.FindAllStringSubmatchIndex(line, -1)
			for _, f := range flags {
				cmd := current
				for _, inv := range invocations {
					if inv[0] < f[2] {
						second := ""
						if inv[4] >= 0 {
							second = line[inv[4]:inv[5]]
						}
						if c := resolveCommand(paths, line[inv[2]:inv[3]], second); c != nil {
							cmd = c
						}
					}
				}
				if cmd == nil {
					continue
				}
				flag := strings.TrimPrefix(line[f[2]:f[3]], "--")
				if !allFlags(cmd)[flag] && flag != "help" && flag != "version" {
					t.Errorf("%s:%d: `--%s` is not a flag of `loom %s`", name, n+1, flag, cmd.CommandPath()[len("loom "):])
				}
			}
		}
	}
}

// a fenced block: ```lang? \n body ```
var fenceRe = regexp.MustCompile("(?s)```([a-z]*)\\n(.*?)```")

// a declaration line: prompt X {, prompt X inherits A, B {, block X {, overlay X {
var declRe = regexp.MustCompile(`(?m)^(?:prompt|block|overlay) [A-Za-z0-9_.-]+(?: inherits [A-Za-z0-9_., -]+)? \{\s*$`)

// Every complete prompt/block/overlay example in the language reference must parse and, when
// it is self-contained, validate without errors. Examples that are deliberately wrong sit
// under a "legacy" or "error" marker line right above the fence.
func TestDocExamplesParse(t *testing.T) {
	checked := 0
	for _, name := range []string{"LOOM_LANGUAGE.md", "LOOM_COMMAND.md", "../README.md", "PACKMAKER_DESIGN.md"} {
		doc := readDoc(t, name)
		for _, m := range fenceRe.FindAllStringSubmatchIndex(doc, -1) {
			lang, src := doc[m[2]:m[3]], doc[m[4]:m[5]]
			if (lang != "" && lang != "loom" && lang != "dsl") || !declRe.MatchString(src) {
				continue
			}
			line := 1 + strings.Count(doc[:m[0]], "\n")
			before := strings.ToLower(doc[max(0, m[0]-200):m[0]])
			if strings.Contains(before, "legacy") || strings.Contains(before, "error:") || strings.Contains(before, "no longer") {
				continue
			}
			if first := strings.ToLower(strings.TrimSpace(strings.SplitN(src, "\n", 2)[0])); strings.HasPrefix(first, "# v1") || strings.HasPrefix(first, "// v1") {
				continue // the "before" half of a migration example
			}
			if hasPlaceholderLine(src) {
				continue // "... fields and declarations ..." style sketches
			}
			checked++
			if _, err := parser.Parse(name, src); err != nil {
				t.Errorf("%s:%d example does not parse: %v\n%s", name, line, err, firstLines(src, 6))
			}
		}
	}
	if checked < 20 {
		t.Errorf("only %d examples were checked; the extraction pattern has probably stopped matching", checked)
	}
	t.Logf("%d DSL examples parsed", checked)
}

func hasPlaceholderLine(src string) bool {
	for _, l := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "...") {
			return true
		}
	}
	return false
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// Every flag a command defines must be mentioned in its section of the command reference.
func TestEveryFlagIsDocumented(t *testing.T) {
	doc := readDoc(t, "LOOM_COMMAND.md")
	sections := commandSections(doc)
	for path, c := range commandPaths(rootCmd) {
		body, ok := sections[path]
		if !ok {
			// documented under its group (loom thread block ...): use the group's section
			for k := len(strings.Fields(path)) - 1; k > 0 && !ok; k-- {
				body, ok = sections[strings.Join(strings.Fields(path)[:k], " ")]
			}
		}
		if !ok {
			continue // reported by TestEveryCommandIsDocumented
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" || f.Hidden {
				return
			}
			if !strings.Contains(body, "--"+f.Name) {
				t.Errorf("`loom %s` has --%s (%s) but its section in LOOM_COMMAND.md never mentions it", path, f.Name, f.Usage)
			}
		})
	}
}
