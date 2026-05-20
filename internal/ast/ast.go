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
	OpAppend                   // +=  — append to inherited value (deprecated; use from() in v2)
	OpRemove                   // -=  — remove items from inherited list (deprecated; no v2 equivalent)
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

// SubscriptKind distinguishes the three subscript notations inside a from() expression.
type SubscriptKind int

const (
	SubAll   SubscriptKind = iota // [*]     — all items
	SubIndex                      // [N]     — single item at 0-based index N
	SubRange                      // [N..M]  — items N..(M-1), exclusive end
)

// Subscript is a parsed subscript inside a from() expression.
type Subscript struct {
	Kind SubscriptKind
	N    int // SubIndex: the index; SubRange: start (inclusive)
	M    int // SubRange: end (exclusive)
}

// FromUnitKind distinguishes the four forms a from() unit can take.
type FromUnitKind int

const (
	// from(parent[sub]) — pull field values from indexed parents
	FromParentRef FromUnitKind = iota
	// from(pack.Name) or from(BareName) — pull from a specific named parent
	FromNamedRef
	// parent[sub].fieldName[sub] — explicit source + field + subscript
	FromFieldRef
	// { - item ... } — literal inline items to append
	FromLiteral
)

// FromUnit is one segment of a from() expression, joined by "and".
type FromUnit struct {
	Kind FromUnitKind
	// FromParentRef: which parent(s) to pull the current field from
	ParentSub Subscript
	// FromNamedRef: the named parent reference (bare or slug.Name)
	ParentName string
	// FromFieldRef: source parent subscript, field name, and field subscript
	SourceSub Subscript
	FieldName string
	FieldSub  Subscript
	// FromLiteral: the inline items (bullet prefixes already stripped)
	Items []string
	Pos   Position
}

// FromExpression is a parsed from() expression on the RHS of a := field assignment.
type FromExpression struct {
	Units []FromUnit
	Pos   Position
}

// FieldOperation is one field assignment in a prompt or block body.
// Value holds raw content lines as emitted by the lexer (may include "- " prefix).
// FromExpr is non-nil when the := RHS contains a from() expression.
type FieldOperation struct {
	FieldName string
	Op        Operator
	Value     []string
	FromExpr  *FromExpression // non-nil when Op==OpOverride and RHS uses from() syntax
	Pos       Position
}

// VarDecl holds a variable or slot declaration from within a prompt body.
type VarDecl struct {
	Name     string
	Default  string
	IsSlot   bool // declared with 'slot' — triggers interactive prompting
	Required bool // true when no default is given and field is required
	Secret   bool // slot api_key { secret: true } — value must not appear in plain-text output
	Pos      Position
}

// EnvBlock holds environment-specific field operations declared with `env <name> { ... }`.
type EnvBlock struct {
	Name   string
	Fields []FieldOperation
	Pos    Position
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
	Kind    NodeKind
	Name    string
	// Parent holds the first (or only) parent name for backward compatibility.
	// For single-parent prompts it equals Parents[0]; for base prompts it is "".
	Parent  string
	// Parents holds all declared parent names in order (v2 multiple inheritance).
	// len==0 for base prompts, len==1 for single-parent, len>1 for multi-parent.
	Parents      []string
	Uses         []string // ordered block names from "use BlockName" statements
	Fields       []FieldOperation
	Pos          Position
	Vars         []VarDecl
	Variants     []VariantBlock
	EnvBlocks    []EnvBlock
	Contract     *ContractBlock
	Capabilities *CapabilitiesBlock

	// M22 fields
	NodeKindTag    string   // value of the `kind:` scalar field, if set
	Todo           []string // items from the `todo:` list field
	CompatibleWith []string // items from `compatible_with:` list field
}

// ScalarFields contains field names whose value is a single string.
var ScalarFields = map[string]bool{
	"summary": true, "persona": true, "context": true,
	"objective": true, "notes": true,
	"kind": true,
}

// ListFields contains field names whose value is an ordered list of strings.
var ListFields = map[string]bool{
	"instructions": true, "constraints": true,
	"examples": true, "format": true,
	"todo": true, "compatible_with": true,
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
	// AppliedEnv is the env block name applied during resolution, if any.
	AppliedEnv string
	// UnresolvedTokens lists any {{ token }} placeholders left after substitution.
	UnresolvedTokens []string
	// Fingerprint is the stable SHA-256 fingerprint of the resolved prompt fields.
	Fingerprint string
	// Warnings holds non-fatal resolution notices (e.g. multi-parent field conflicts).
	Warnings []string
	// AllEnvBlocks accumulates env blocks from the full inheritance chain (root-first).
	// Used by the resolver to apply opts.Env; not rendered.
	AllEnvBlocks []EnvBlock

	// M22 fields
	Kind           string   // value of `kind:` scalar field
	Todo           []string // items from `todo:` list field
	CompatibleWith []string // items from `compatible_with:` list field
}
