// Package blame provides git-integrated field attribution for loom prompts.
// It maps each resolved field item back to the git commit that last touched
// the source line, using `git blame --line-porcelain`.
package blame

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// CommitInfo holds attribution data from git blame.
type CommitInfo struct {
	Hash    string
	Author  string
	Date    time.Time
	Summary string
}

// FieldItem is one attributed value from a resolved field.
type FieldItem struct {
	Value     string
	File      string // relative path to source file
	Line      int
	Origin    string // "direct", "inherited", "block"
	Commit    CommitInfo
	Untracked bool // file is not tracked by git
}

// Result holds the blame output for one prompt.
type Result struct {
	PromptName string
	Field      string
	Items      []FieldItem
}

// RunBlame returns blame attribution for the given prompt and optional field filter.
// cwd is the project root. since is an optional date/ref filter (empty = all history).
func RunBlame(promptName, fieldFilter, since, instructionFilter, cwd string) ([]Result, error) {
	if err := requireGitRepo(cwd); err != nil {
		return nil, err
	}

	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return nil, fmt.Errorf("load project: %w", err)
	}
	_ = cfg

	rp, err := resolve.Resolve(promptName, reg)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", promptName, err)
	}

	var results []Result

	fields := fieldsToBlame(fieldFilter)
	for _, fieldName := range fields {
		items, err := blameField(rp, fieldName, cwd)
		if err != nil {
			return nil, err
		}

		if instructionFilter != "" {
			var filtered []FieldItem
			for _, it := range items {
				if strings.Contains(strings.ToLower(it.Value), strings.ToLower(instructionFilter)) {
					filtered = append(filtered, it)
				}
			}
			items = filtered
		}

		if since != "" {
			items = filterSince(items, since)
		}

		if len(items) > 0 {
			results = append(results, Result{
				PromptName: promptName,
				Field:      fieldName,
				Items:      items,
			})
		}
	}
	return results, nil
}

func fieldsToBlame(filter string) []string {
	all := []string{"summary", "persona", "context", "objective", "instructions", "constraints", "examples", "format", "notes"}
	if filter == "" {
		return all
	}
	for _, f := range all {
		if f == filter {
			return []string{f}
		}
	}
	return []string{filter}
}

func blameField(rp *ast.ResolvedPrompt, fieldName, cwd string) ([]FieldItem, error) {
	scalarFields := map[string]bool{
		"summary": true, "persona": true, "context": true, "objective": true, "notes": true,
	}

	if scalarFields[fieldName] {
		contrib, ok := rp.ScalarSources[fieldName]
		if !ok || contrib.Pos.File == "" {
			return nil, nil
		}
		item := FieldItem{
			Value:  contrib.Value,
			File:   relativize(contrib.Pos.File, cwd),
			Line:   contrib.Pos.Line,
			Origin: originLabel(contrib),
		}
		if err := attachCommit(&item, contrib.Pos.File, contrib.Pos.Line); err != nil {
			item.Untracked = true
		}
		return []FieldItem{item}, nil
	}

	// List field.
	contribs := rp.ListSources[fieldName]
	var items []FieldItem
	for _, contrib := range contribs {
		if contrib.Pos.File == "" {
			continue
		}
		item := FieldItem{
			Value:  contrib.Value,
			File:   relativize(contrib.Pos.File, cwd),
			Line:   contrib.Pos.Line,
			Origin: originLabel(contrib),
		}
		if err := attachCommit(&item, contrib.Pos.File, contrib.Pos.Line); err != nil {
			item.Untracked = true
		}
		items = append(items, item)
	}
	return items, nil
}

func originLabel(c ast.SourceContribution) string {
	if c.FromBlock {
		return "block composition"
	}
	if c.Op == ast.OpDefine || c.Op == ast.OpOverride {
		return "direct"
	}
	return "inherited"
}

// attachCommit runs `git blame --line-porcelain -L line,line -- file` and populates commit info.
func attachCommit(item *FieldItem, absFile string, line int) error {
	if line <= 0 {
		return fmt.Errorf("invalid line")
	}
	lineRange := fmt.Sprintf("%d,%d", line, line)
	cmd := exec.Command("git", "blame", "--line-porcelain", "-L", lineRange, "--", absFile)
	cmd.Dir = filepath.Dir(absFile)
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	info, err := parseLinePorcelain(out)
	if err != nil {
		return err
	}
	item.Commit = info
	return nil
}

// parseLinePorcelain parses git blame --line-porcelain output for a single line.
func parseLinePorcelain(data []byte) (CommitInfo, error) {
	var ci CommitInfo
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case len(line) >= 40 && !strings.HasPrefix(line, "\t") && !strings.Contains(line[:40], " "):
			// First line of a block: <hash> <orig> <final> [count]
			fields := strings.Fields(line)
			if len(fields) >= 1 && len(fields[0]) == 40 {
				ci.Hash = fields[0][:7]
			}
		case strings.HasPrefix(line, "author ") && ci.Author == "":
			ci.Author = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			ts, _ := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			if ts > 0 {
				ci.Date = time.Unix(ts, 0).UTC()
			}
		case strings.HasPrefix(line, "summary "):
			ci.Summary = strings.TrimPrefix(line, "summary ")
		}
	}
	if ci.Hash == "" {
		return ci, fmt.Errorf("no blame data parsed")
	}
	return ci, nil
}

func filterSince(items []FieldItem, since string) []FieldItem {
	var cutoff time.Time
	// Try as a date first (YYYY-MM-DD or YYYY-MM-DDTHH:MM:SS).
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, since); err == nil {
			cutoff = t
			break
		}
	}

	if cutoff.IsZero() {
		// Treat as a git ref — resolve to a timestamp.
		out, err := exec.Command("git", "log", "-1", "--format=%at", since).Output()
		if err == nil {
			ts, _ := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			if ts > 0 {
				cutoff = time.Unix(ts, 0).UTC()
			}
		}
	}

	if cutoff.IsZero() {
		return items
	}

	var filtered []FieldItem
	for _, it := range items {
		if it.Untracked || it.Commit.Date.After(cutoff) || it.Commit.Date.Equal(cutoff) {
			filtered = append(filtered, it)
		}
	}
	return filtered
}

func relativize(absPath, root string) string {
	if root == "" {
		return absPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return rel
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func requireGitRepo(cwd string) error {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = cwd
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("not a git repository — loom blame requires git")
	}
	return nil
}
