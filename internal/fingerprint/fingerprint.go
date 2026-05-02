package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

type payload struct {
	Summary      string   `json:"summary,omitempty"`
	Persona      string   `json:"persona,omitempty"`
	Context      string   `json:"context,omitempty"`
	Objective    string   `json:"objective,omitempty"`
	Instructions []string `json:"instructions,omitempty"`
	Constraints  []string `json:"constraints,omitempty"`
	Examples     []string `json:"examples,omitempty"`
	Format       []string `json:"format,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

func Compute(rp *ast.ResolvedPrompt) (string, error) {
	body, err := json.Marshal(payload{
		Summary:      rp.Summary,
		Persona:      rp.Persona,
		Context:      rp.Context,
		Objective:    rp.Objective,
		Instructions: rp.Instructions,
		Constraints:  rp.Constraints,
		Examples:     rp.Examples,
		Format:       rp.Format,
		Notes:        rp.Notes,
	})
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
