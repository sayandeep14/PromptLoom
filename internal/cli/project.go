package cli

import (
	"os"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
)

func resolveProjectDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if projectDir, ok := config.FindProjectRoot(cwd); ok {
		return projectDir, nil
	}
	return cwd, nil
}

// fieldValues returns the values of all FieldOperations with the given name,
// stripping leading "- " bullet prefixes.
func fieldValues(fields []ast.FieldOperation, name string) []string {
	var out []string
	for _, f := range fields {
		if f.FieldName == name {
			for _, v := range f.Value {
				out = append(out, strings.TrimPrefix(v, "- "))
			}
		}
	}
	return out
}
