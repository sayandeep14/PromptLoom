package diff

import "github.com/sayandeepgiri/promptloom/internal/ast"

// FieldDiff holds the computed difference for one field between two resolved prompts.
type FieldDiff struct {
	Field   string // internal name: "constraints", "objective", etc.
	Heading string // display name: "Constraints", "Objective", etc.
	IsList  bool
	Added   []string
	Removed []string
	Before  string // scalar before (empty if unchanged or list)
	After   string // scalar after
	Changed bool   // true if anything changed
}

// fieldSpec describes a field's internal name, display heading, and list/scalar type.
type fieldSpec struct {
	name    string
	heading string
	isList  bool
}

var canonicalOrder = []fieldSpec{
	{"summary", "Summary", false},
	{"persona", "Persona", false},
	{"context", "Context", false},
	{"objective", "Objective", false},
	{"instructions", "Instructions", true},
	{"constraints", "Constraints", true},
	{"examples", "Examples", true},
	{"format", "Output Format", true},
	{"notes", "Notes", false},
	{"inheritance", "Inheritance", false},
}

// DiffPrompts computes field-by-field differences between two resolved prompts.
func DiffPrompts(a, b *ast.ResolvedPrompt) []FieldDiff {
	var diffs []FieldDiff
	for _, spec := range canonicalOrder {
		var fd FieldDiff
		fd.Field = spec.name
		fd.Heading = spec.heading
		fd.IsList = spec.isList

		if spec.name == "inheritance" {
			before := joinChain(a.InheritsChain)
			after := joinChain(b.InheritsChain)
			if before != after {
				fd.Before = before
				fd.After = after
				fd.Changed = true
			}
			diffs = append(diffs, fd)
			continue
		}

		if spec.isList {
			aItems := getList(a, spec.name)
			bItems := getList(b, spec.name)
			added, removed := setDiff(aItems, bItems)
			fd.Added = added
			fd.Removed = removed
			fd.Changed = len(added) > 0 || len(removed) > 0
		} else {
			av := getScalar(a, spec.name)
			bv := getScalar(b, spec.name)
			if av != bv {
				fd.Before = av
				fd.After = bv
				fd.Changed = true
			}
		}
		diffs = append(diffs, fd)
	}
	return diffs
}

// setDiff returns items in b not in a (added) and items in a not in b (removed).
func setDiff(a, b []string) (added, removed []string) {
	aSet := make(map[string]bool, len(a))
	bSet := make(map[string]bool, len(b))
	for _, v := range a {
		aSet[v] = true
	}
	for _, v := range b {
		bSet[v] = true
	}
	for _, v := range b {
		if !aSet[v] {
			added = append(added, v)
		}
	}
	for _, v := range a {
		if !bSet[v] {
			removed = append(removed, v)
		}
	}
	return added, removed
}

func joinChain(chain []string) string {
	out := ""
	for i, c := range chain {
		if i > 0 {
			out += " → "
		}
		out += c
	}
	return out
}

func getScalar(rp *ast.ResolvedPrompt, name string) string {
	switch name {
	case "summary":
		return rp.Summary
	case "persona":
		return rp.Persona
	case "context":
		return rp.Context
	case "objective":
		return rp.Objective
	case "notes":
		return rp.Notes
	}
	return ""
}

func getList(rp *ast.ResolvedPrompt, name string) []string {
	switch name {
	case "instructions":
		return rp.Instructions
	case "constraints":
		return rp.Constraints
	case "examples":
		return rp.Examples
	case "format":
		return rp.Format
	}
	return nil
}
