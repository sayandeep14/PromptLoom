package sourcemap

import (
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

type entry struct {
	Value  string `json:"value,omitempty"`
	Source string `json:"source"`
	Line   int    `json:"line"`
	Op     string `json:"op"`
}

type document struct {
	Prompt      string                 `json:"prompt"`
	Fingerprint string                 `json:"fingerprint"`
	RenderedAt  string                 `json:"rendered_at"`
	Fields      map[string]interface{} `json:"fields"`
}

func Build(rp *ast.ResolvedPrompt, renderedAt time.Time, projectRoot string) ([]byte, error) {
	doc := document{
		Prompt:      rp.Name,
		Fingerprint: rp.Fingerprint,
		RenderedAt:  renderedAt.UTC().Format(time.RFC3339),
		Fields:      map[string]interface{}{},
	}

	for _, fieldName := range []string{"summary", "persona", "context", "objective", "notes"} {
		contrib, ok := rp.ScalarSources[fieldName]
		if !ok || contrib.Source == "" {
			continue
		}
		doc.Fields[fieldName] = toEntry(contrib, projectRoot)
	}

	for _, fieldName := range []string{"instructions", "constraints", "examples", "format"} {
		contribs := rp.ListSources[fieldName]
		if len(contribs) == 0 {
			continue
		}
		items := make([]entry, 0, len(contribs))
		for _, contrib := range contribs {
			items = append(items, toEntry(contrib, projectRoot))
		}
		doc.Fields[fieldName] = items
	}

	return json.MarshalIndent(doc, "", "  ")
}

func toEntry(contrib ast.SourceContribution, projectRoot string) entry {
	return entry{
		Value:  contrib.Value,
		Source: relativize(contrib.Pos.File, projectRoot),
		Line:   contrib.Pos.Line,
		Op:     opName(contrib.Op, contrib.FromBlock),
	}
}

func relativize(path, root string) string {
	if path == "" || root == "" {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func opName(op ast.Operator, fromBlock bool) string {
	if fromBlock {
		return "block"
	}
	switch op {
	case ast.OpDefine:
		return "define"
	case ast.OpOverride:
		return "override"
	case ast.OpAppend:
		return "append"
	case ast.OpRemove:
		return "remove"
	}
	return "unknown"
}
