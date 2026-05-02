package ast

import "fmt"

// NodeKind distinguishes prompt declarations from block declarations.
type NodeKind int

const (
	KindPrompt NodeKind = iota
	KindBlock
	KindOverlay
)

// Operator is the merge operator on a field declaration.
type Operator int

const (
	OpDefine   Operator = iota // :   — set field value (override if inherited)
	OpOverride                 // :=  — unconditionally replace inherited value
	OpAppend                   // +=  — append to inherited value
	OpRemove                   // -=  — remove items from inherited list
)

func (o Operator) String() string {
	switch o {
	case OpDefine:
		return ":"
	case OpOverride:
		return ":="
	case OpAppend:
		return "+="
	case OpRemove:
		return "-="
	}
	return "?"
}

// Position records the source location of a token or node.
type Position struct {
	File string
	Line int
	Col  int
}

func (p Position) String() string {
	if p.Col > 0 {
		return fmt.Sprintf("%s:%d:%d", p.File, p.Line, p.Col)
	}
	return fmt.Sprintf("%s:%d", p.File, p.Line)
}

// FieldOperation is one field assignment in a prompt or block body.
// Value holds raw content lines as emitted by the lexer (may include "- " prefix).
type FieldOperation struct {
	FieldName string
	Op        Operator
	Value     []string
	Pos       Position
}

// VarDecl holds a variable or slot declaration from within a prompt body.
type VarDecl struct {
	Name     string
	Default  string
	IsSlot   bool // declared with 'slot' — triggers interactive prompting
	Required bool // true when no default is given and field is required
	Pos      Position
}

// VariantBlock holds a named set of field operations to apply as an alternative.
type VariantBlock struct {
	Name   string
	Fields []FieldOperation
	Pos    Position
}

// ContractBlock holds constraints that must be satisfied by the rendered output.
type ContractBlock struct {
	RequiredSections  []string
	ForbiddenSections []string
	MustInclude       []string
	MustNotInclude    []string
	Pos               Position
}

// CapabilitiesBlock declares allowed and forbidden capabilities for the prompt.
type CapabilitiesBlock struct {
	Allowed   []string
	Forbidden []string
	Pos       Position
}

// Node is a parsed prompt or block declaration.
type Node struct {
	Kind         NodeKind
	Name         string
	Parent       string   // set for prompts with "inherits Parent"
	Uses         []string // ordered block names from "use BlockName" statements
	Fields       []FieldOperation
	Pos          Position
	Vars         []VarDecl
	Variants     []VariantBlock
	Contract     *ContractBlock
	Capabilities *CapabilitiesBlock
}

// ScalarFields contains field names whose value is a single string.
var ScalarFields = map[string]bool{
	"summary": true, "persona": true, "context": true,
	"objective": true, "notes": true,
}

// ListFields contains field names whose value is an ordered list of strings.
var ListFields = map[string]bool{
	"instructions": true, "constraints": true,
	"examples": true, "format": true,
}

// ValidFields is the union of ScalarFields and ListFields.
var ValidFields map[string]bool

func init() {
	ValidFields = make(map[string]bool)
	for k := range ScalarFields {
		ValidFields[k] = true
	}
	for k := range ListFields {
		ValidFields[k] = true
	}
}

// TraceEntry records a single contribution to a resolved field.
type TraceEntry struct {
	Op        Operator // the operator that was applied
	Source    string   // name of the prompt or block that applied it
	Pos       Position
	FromBlock bool
}

// SourceContribution records the source metadata for a resolved field or list item.
type SourceContribution struct {
	FieldName string
	Value     string
	Source    string
	Pos       Position
	Op        Operator
	FromBlock bool
}

// ResolvedPrompt is the fully resolved state of a prompt after all inheritance,
// block composition, and field operations have been applied.
type ResolvedPrompt struct {
	Name         string
	Summary      string
	Persona      string
	Context      string
	Objective    string
	Instructions []string
	Constraints  []string
	Examples     []string
	Format       []string
	Notes        string

	// SourceTrace maps each field name to the name of the node that last set it.
	SourceTrace map[string]string

	// FullTrace records every contribution to each field in application order.
	FullTrace map[string][]TraceEntry
	// ScalarSources records the last effective source for each scalar field.
	ScalarSources map[string]SourceContribution
	// ListSources records the effective source for each rendered list item.
	ListSources map[string][]SourceContribution

	// InheritsChain is the ordered list of ancestor prompt names, root first.
	InheritsChain []string
	// UsedBlocks is the deduplicated ordered list of all blocks applied during resolution.
	UsedBlocks []string

	// Vars holds the var/slot declarations from the target node only (not inherited).
	Vars []VarDecl
	// Variants holds all variant blocks from the target node.
	Variants []VariantBlock
	// VarValues holds the effective variable values used during substitution.
	VarValues map[string]string
	// AppliedVariant is the selected variant name, if any.
	AppliedVariant string
	// AppliedOverlays records the overlays applied at render time, in order.
	AppliedOverlays []string
	// UnresolvedTokens lists any {{ token }} placeholders left after substitution.
	UnresolvedTokens []string
	// Fingerprint is the stable SHA-256 fingerprint of the resolved prompt fields.
	Fingerprint string
}
