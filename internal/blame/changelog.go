package blame

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

// ChangeEntry is one human-readable changelog line for a prompt.
type ChangeEntry struct {
	Date    time.Time
	Author  string
	Message string
}

// PromptChangelog holds all change entries for one prompt name.
type PromptChangelog struct {
	Name    string
	Entries []ChangeEntry
}

// BuildChangelog returns per-prompt change history.
// since may be a date (YYYY-MM-DD) or a git ref (HEAD~5); empty = all history.
// promptFilter limits output to one named prompt; empty = all prompts.
func BuildChangelog(cwd, since, promptFilter string) ([]PromptChangelog, error) {
	if err := requireGitRepo(cwd); err != nil {
		return nil, err
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}

	commits, err := gitLogCommits(cwd, since, sourceDirs(cwd))
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	// Map: prompt name → ordered change entries.
	byPrompt := map[string][]ChangeEntry{}

	for _, c := range commits {
		files, err := commitChangedFiles(cwd, c.hash)
		if err != nil {
			continue
		}
		for _, relFile := range files {
			if !isLoomSource(relFile) {
				continue
			}
			absFile := filepath.Join(cwd, relFile)
			before := gitShowFile(cwd, c.hash+"~1", relFile)
			after := gitShowFile(cwd, c.hash, relFile)

			affected := entriesForFileDiff(relFile, absFile, before, after, c, reg)
			for pname, entries := range affected {
				if promptFilter != "" && pname != promptFilter {
					continue
				}
				byPrompt[pname] = append(byPrompt[pname], entries...)
			}
		}
	}

	// Output in registry order, then any stale/deleted prompts found only in history.
	seen := map[string]bool{}
	var result []PromptChangelog
	for _, node := range reg.Prompts() {
		seen[node.Name] = true
		entries, ok := byPrompt[node.Name]
		if !ok || (promptFilter != "" && node.Name != promptFilter) {
			continue
		}
		result = append(result, PromptChangelog{Name: node.Name, Entries: entries})
	}
	var gone []string
	for name := range byPrompt {
		if !seen[name] && (promptFilter == "" || promptFilter == name) {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		result = append(result, PromptChangelog{Name: name, Entries: byPrompt[name]})
	}
	return result, nil
}

// sourceDirs are the configured prompt, block and overlay directories, relative to cwd (the
// defaults when there is no loom.toml).
func sourceDirs(cwd string) []string {
	dirs := []string{"prompts", "blocks", "overlays"}
	if cfg, err := config.Load(cwd); err == nil {
		dirs = nil
		for _, d := range []string{cfg.Paths.Prompts, cfg.Paths.Blocks, cfg.Paths.Overlays} {
			if d != "" {
				dirs = append(dirs, filepath.ToSlash(d))
			}
		}
	}
	return dirs
}

// gitCommit holds minimal info about one commit.
type gitCommit struct {
	hash    string
	author  string
	date    time.Time
	subject string
}

func gitLogCommits(cwd, since string, dirs []string) ([]gitCommit, error) {
	// \x1f (unit separator) cannot appear in an author name or subject, unlike "|".
	args := []string{"log", "--format=%H%x1f%an%x1f%aI%x1f%s"}
	if since != "" {
		if strings.HasPrefix(since, "-") {
			return nil, fmt.Errorf("invalid --since value %q: expected a date (YYYY-MM-DD) or a git ref", since)
		}
		if looksLikeDate(since) {
			// git fills a bare date with the CURRENT time of day, so "--since 2024-01-03" would
			// mean different things at 9am and at 5pm. A date means the start of that day.
			if len(since) == len("2006-01-02") {
				since += " 00:00:00"
			}
			args = append(args, "--since="+since)
		} else {
			args = append(args, since+"..HEAD")
		}
	}
	// Scope to the project's loom source directories only.
	args = append(args, "--")
	args = append(args, dirs...)

	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		if since != "" {
			// an unknown ref is the caller's mistake, not an empty history
			return nil, fmt.Errorf("cannot read history since %q (not a date or a known git ref)", since)
		}
		// No commits yet — treat as empty, not an error.
		return nil, nil
	}
	var commits []gitCommit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) < 4 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, parts[2])
		commits = append(commits, gitCommit{
			hash:    parts[0],
			author:  parts[1],
			date:    t,
			subject: parts[3],
		})
	}
	return commits, nil
}

func commitChangedFiles(cwd, hash string) ([]string, error) {
	// --root: the first commit has no parent, and without it diff-tree lists nothing for it.
	// -z: paths are NUL-separated and never quoted, so names with spaces or non-ASCII survive
	cmd := exec.Command("git", "diff-tree", "--root", "--no-commit-id", "-r", "--name-only", "-z", hash)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var files []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files, nil
}

func isLoomSource(path string) bool {
	return strings.HasSuffix(path, ".loom") ||
		strings.HasSuffix(path, ".prompt.loom") ||
		strings.HasSuffix(path, ".block.loom") ||
		strings.HasSuffix(path, ".overlay.loom")
}

// entriesForFileDiff computes change entries by comparing AST nodes before and after a commit.
func entriesForFileDiff(relFile, absFile, before, after string, c gitCommit, reg *registry.Registry) map[string][]ChangeEntry {
	result := map[string][]ChangeEntry{}

	beforeNodes := parseNodes(before, relFile)
	afterNodes := parseNodes(after, relFile)

	// Index by name.
	beforeByName := indexByName(beforeNodes)
	afterByName := indexByName(afterNodes)

	// Collect all names that appeared in either version.
	allNames := map[string]bool{}
	for n := range beforeByName {
		allNames[n] = true
	}
	for n := range afterByName {
		allNames[n] = true
	}

	names := make([]string, 0, len(allNames))
	for n := range allNames {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		bNode := beforeByName[name]
		aNode := afterByName[name]

		var entries []ChangeEntry

		switch {
		case bNode == nil && aNode != nil:
			// New prompt/block created.
			if aNode.Kind == ast.KindPrompt {
				entries = append(entries, ChangeEntry{Date: c.date, Author: c.author, Message: fmt.Sprintf("Prompt created  (%s)", c.author)})
			}
		case bNode != nil && aNode == nil:
			// Prompt/block deleted.
			if bNode.Kind == ast.KindPrompt {
				entries = append(entries, ChangeEntry{Date: c.date, Author: c.author, Message: fmt.Sprintf("Prompt deleted  (%s)", c.author)})
			}
		default:
			// Both exist — diff them.
			entries = append(entries, diffNodes(bNode, aNode, c)...)
		}

		if len(entries) == 0 {
			continue
		}

		// Attribute entries to the prompt name. If it's a block, fan out to users.
		if aNode != nil && aNode.Kind == ast.KindBlock {
			for _, pname := range promptsUsingBlock(name, reg) {
				result[pname] = append(result[pname], entries...)
			}
		} else {
			result[name] = append(result[name], entries...)
		}
	}
	return result
}

func parseNodes(src, filename string) []*ast.Node {
	if src == "" {
		return nil
	}
	nodes, err := parser.Parse(filename, src)
	if err != nil {
		return nil
	}
	return nodes
}

func indexByName(nodes []*ast.Node) map[string]*ast.Node {
	m := map[string]*ast.Node{}
	for _, n := range nodes {
		m[n.Name] = n
	}
	return m
}

// diffNodes compares two AST nodes and returns human-readable change entries.
func diffNodes(before, after *ast.Node, c gitCommit) []ChangeEntry {
	var entries []ChangeEntry

	// Inheritance change.
	if parentOrNone(before) != parentOrNone(after) {
		entries = append(entries, ChangeEntry{
			Date:    c.date,
			Author:  c.author,
			Message: fmt.Sprintf("Inheritance changed: %s → %s  (%s)", parentOrNone(before), parentOrNone(after), c.author),
		})
	}

	// Block usage changes.
	added, removed := sliceDiff(before.Uses, after.Uses)
	for _, b := range added {
		entries = append(entries, ChangeEntry{
			Date:    c.date,
			Author:  c.author,
			Message: fmt.Sprintf("Added block %s  (%s)", b, c.author),
		})
	}
	for _, b := range removed {
		entries = append(entries, ChangeEntry{
			Date:    c.date,
			Author:  c.author,
			Message: fmt.Sprintf("Removed block %s  (%s)", b, c.author),
		})
	}

	// Field-level changes.
	beforeFields := fieldMap(before)
	afterFields := fieldMap(after)

	allFields := map[string]bool{}
	for f := range beforeFields {
		allFields[f] = true
	}
	for f := range afterFields {
		allFields[f] = true
	}

	canonicalOrder := []string{"summary", "persona", "context", "objective", "instructions", "constraints", "examples", "format", "notes"}
	for _, field := range canonicalOrder {
		if !allFields[field] {
			continue
		}
		bItems := beforeFields[field]
		aItems := afterFields[field]

		added, removed := sliceDiff(bItems, aItems)
		heading := capitalizeFirst(field)
		if field == "format" {
			heading = "Output Format"
		}

		// A scalar field holds one text: a change is "updated", not a removed and an added line.
		if scalarFields[field] {
			if strings.Join(bItems, "\n") != strings.Join(aItems, "\n") {
				entries = append(entries, ChangeEntry{Date: c.date, Author: c.author, Message: fmt.Sprintf("%s updated  (%s)", heading, c.author)})
			}
			continue
		}

		for _, v := range added {
			entries = append(entries, ChangeEntry{
				Date:    c.date,
				Author:  c.author,
				Message: fmt.Sprintf("%s added: %q  (%s)", heading, truncate(v, 60), c.author),
			})
		}
		for _, v := range removed {
			entries = append(entries, ChangeEntry{
				Date:    c.date,
				Author:  c.author,
				Message: fmt.Sprintf("%s removed: %q  (%s)", heading, truncate(v, 60), c.author),
			})
		}
		// For scalar fields, if the list of values changed at all, just say "updated".
		if len(added) == 0 && len(removed) == 0 {
			bVal := strings.Join(bItems, "\n")
			aVal := strings.Join(aItems, "\n")
			if bVal != aVal {
				entries = append(entries, ChangeEntry{
					Date:    c.date,
					Author:  c.author,
					Message: fmt.Sprintf("%s updated  (%s)", heading, c.author),
				})
			}
		}
	}

	return entries
}

var scalarFields = map[string]bool{"summary": true, "persona": true, "context": true, "objective": true, "notes": true}

// fieldMap returns a map of field name → list of value lines from all field operations.
func fieldMap(node *ast.Node) map[string][]string {
	m := map[string][]string{}
	for _, fo := range node.Fields {
		m[fo.FieldName] = append(m[fo.FieldName], fo.Value...)
	}
	return m
}

func parentOrNone(n *ast.Node) string {
	parents := n.Parents
	if len(parents) == 0 && n.Parent != "" {
		parents = []string{n.Parent}
	}
	if len(parents) == 0 {
		return "(none)"
	}
	return strings.Join(parents, ", ")
}

func sliceDiff(before, after []string) (added, removed []string) {
	bSet := map[string]bool{}
	aSet := map[string]bool{}
	for _, v := range before {
		bSet[v] = true
	}
	for _, v := range after {
		aSet[v] = true
	}
	for _, v := range after {
		if !bSet[v] {
			added = append(added, v)
		}
	}
	for _, v := range before {
		if !aSet[v] {
			removed = append(removed, v)
		}
	}
	return
}

func promptsUsingBlock(blockName string, reg *registry.Registry) []string {
	var names []string
	for _, p := range reg.Prompts() {
		for _, u := range p.Uses {
			if u == blockName {
				names = append(names, p.Name)
				break
			}
		}
	}
	return names
}

func gitShowFile(cwd, ref, relFile string) string {
	cmd := exec.Command("git", "show", ref+":"+relFile)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func looksLikeDate(s string) bool {
	return len(s) >= 10 && s[4] == '-' && s[7] == '-'
}
