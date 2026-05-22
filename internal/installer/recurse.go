package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/deps"
)

// Conflict describes a version conflict between two packs' requirements for the
// same dependency.
type Conflict struct {
	Slug    string
	Entries []ConflictEntry
}

func (c Conflict) Error() string {
	var parts []string
	for _, e := range c.Entries {
		parts = append(parts, fmt.Sprintf("%s (required by %s)", e.Constraint, e.RequiredBy))
	}
	return fmt.Sprintf("version conflict for %q: %s", c.Slug, strings.Join(parts, " vs "))
}

// ConflictEntry is one constraint on a conflicting pack.
type ConflictEntry struct {
	Constraint string // e.g. ">=1.0.0"
	RequiredBy string // slug of the requiring pack, or "" for the top-level request
}

// DepResult bundles a single install result with the pack slug for reporting.
type DepResult struct {
	*Result
	DirectRequest bool // true when this was the slug the user asked for
}

// requirement tracks all constraints imposed on one pack slug across the dep graph.
type requirement struct {
	constraints []ConflictEntry
}

// InstallWithDeps installs slug and all its transitive dependencies, then writes
// loompack.lock. Returns results for every pack installed (direct + transitive),
// any version conflicts detected, and any fatal error.
//
// Version conflicts are reported as non-nil Conflicts alongside a non-nil error
// so callers can display all conflicts before exiting.
func InstallWithDeps(slug, cwd string) ([]*DepResult, []Conflict, error) {
	packLock, err := deps.ReadPackLock(cwd)
	if err != nil {
		return nil, nil, fmt.Errorf("reading loompack.lock: %w", err)
	}

	// requirements[slug] = all constraints on that slug from the dep graph.
	requirements := map[string]*requirement{}
	// Add the top-level request with no constraint (any version).
	requirements[slug] = &requirement{
		constraints: []ConflictEntry{{Constraint: "any", RequiredBy: "(direct)"}},
	}

	var results []*DepResult
	visited := map[string]bool{} // slugs fully processed

	if err := installRecursive(slug, "(direct)", cwd, requirements, visited, &results, packLock); err != nil {
		return results, nil, err
	}

	conflicts := detectConflicts(packLock, requirements)

	if err := deps.WritePackLock(packLock, cwd); err != nil {
		return results, conflicts, fmt.Errorf("writing loompack.lock: %w", err)
	}

	// Mark the top-level result as direct.
	for _, r := range results {
		if r.Meta.Slug == slug {
			r.DirectRequest = true
		}
	}

	return results, conflicts, nil
}

// installRecursive installs slug (if not already installed at a satisfying version),
// then recurses into its declared dependencies.
func installRecursive(
	slug, requiredBy, cwd string,
	requirements map[string]*requirement,
	visited map[string]bool,
	results *[]*DepResult,
	packLock *deps.PackLock,
) error {
	if visited[slug] {
		return nil
	}
	visited[slug] = true

	// Check if already installed at a satisfying version AND present on disk.
	existing := packLock.Find(slug)
	reqs := requirements[slug]
	if existing != nil && reqs != nil && allSatisfied(existing.Version, reqs.constraints) {
		// Only skip if the pack directory actually exists on disk.
		// If it was deleted or never written, fall through to reinstall.
		if _, err := os.Stat(PackDir(slug, cwd)); err == nil {
			return nil
		}
	}

	result, err := Install(slug, cwd)
	if err != nil {
		return fmt.Errorf("installing %s: %w", slug, err)
	}
	*results = append(*results, &DepResult{Result: result})

	// Update the lock with the newly installed version.
	var requiredByList []string
	if requiredBy != "(direct)" {
		requiredByList = []string{requiredBy}
	}
	packLock.Upsert(deps.PackLockEntry{
		Slug:       result.Meta.Slug,
		Version:    result.Meta.Version,
		RequiredBy: requiredByList,
	})

	// Parse the pack's own .dependency.loom from its source dir.
	depFile := filepath.Join(result.SourceDir, deps.Filename)
	packDeps, err := deps.ParseFile(depFile)
	if err != nil {
		// Pack has no .dependency.loom — no transitive deps.
		return nil
	}

	for _, d := range packDeps {
		constraint := d.Op + d.Version
		if constraint == "" {
			constraint = "any"
		}
		req, ok := requirements[d.Name]
		if !ok {
			req = &requirement{}
			requirements[d.Name] = req
		}
		req.constraints = append(req.constraints, ConflictEntry{
			Constraint: constraint,
			RequiredBy: slug,
		})

		if err := installRecursive(d.Name, slug, cwd, requirements, visited, results, packLock); err != nil {
			return err
		}
	}
	return nil
}

// allSatisfied reports whether installedVersion satisfies every constraint.
func allSatisfied(installedVersion string, constraints []ConflictEntry) bool {
	for _, c := range constraints {
		if c.Constraint == "any" {
			continue
		}
		op, ver, ok := splitConstraint(c.Constraint)
		if !ok || !deps.Satisfies(installedVersion, op, ver) {
			return false
		}
	}
	return true
}

// detectConflicts checks that the installed versions in packLock satisfy all
// accumulated requirements. Returns one Conflict per slug that fails.
func detectConflicts(packLock *deps.PackLock, requirements map[string]*requirement) []Conflict {
	var conflicts []Conflict
	for slug, req := range requirements {
		if len(req.constraints) <= 1 {
			continue
		}
		entry := packLock.Find(slug)
		if entry == nil {
			continue
		}
		var failing []ConflictEntry
		for _, c := range req.constraints {
			if c.Constraint == "any" {
				continue
			}
			op, ver, ok := splitConstraint(c.Constraint)
			if ok && !deps.Satisfies(entry.Version, op, ver) {
				failing = append(failing, c)
			}
		}
		if len(failing) > 0 {
			conflicts = append(conflicts, Conflict{Slug: slug, Entries: failing})
		}
	}
	return conflicts
}

// splitConstraint splits e.g. ">=1.0.0" into (">=", "1.0.0", true).
func splitConstraint(c string) (op, ver string, ok bool) {
	for _, o := range []string{"==", ">=", "~=", "<=", ">", "<"} {
		if strings.HasPrefix(c, o) {
			return o, strings.TrimPrefix(c, o), true
		}
	}
	return "", "", false
}

func appendUnique(ss []string, s string) []string {
	for _, v := range ss {
		if v == s {
			return ss
		}
	}
	return append(ss, s)
}
