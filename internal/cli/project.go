package cli

import (
	"os"

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
