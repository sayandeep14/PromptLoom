package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/deps"
	"github.com/sayandeepgiri/promptloom/internal/export"
	"github.com/sayandeepgiri/promptloom/internal/secret"
	"github.com/spf13/cobra"
)

var initSample bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new PromptLoom project",
	Long: `Create the standard loom/ workspace structure:

  loom/
    src/
      prompts/          ← your .prompt.loom files
      blocks/           ← reusable .block.loom files
      overlays/         ← .overlay.loom files
      .export.loom      ← which prompts to publish
      .dependency.loom  ← prompt-pack dependencies
    loompack/           ← installed packs (loom install)
    context/
      REPO.md           ← project architecture notes
      TODO.md           ← current tasks for loom start
      docs/
    .loom.env
    .loom.config        ← loomlocker config
    .loom.secret        ← API keys (gitignored)
  loom.toml             ← PromptLoom project config`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initSample, "sample", false, "write sample prompts and blocks")
}

func runInit(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	tomlPath := filepath.Join(cwd, "loom.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		return fmt.Errorf("loom.toml already exists — run inside a new project directory")
	}

	loomDir := filepath.Join(cwd, "loom")

	// ── Directory tree ──────────────────────────────────────────────────────
	dirs := []string{
		"loom/src/prompts",
		"loom/src/blocks",
		"loom/src/overlays",
		"loom/loompack",
		"loom/context/docs",
	}
	for _, d := range dirs {
		full := filepath.Join(cwd, d)
		if err := os.MkdirAll(full, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
		fmt.Printf("  created  %s/\n", d)
	}

	// ── loom.toml ───────────────────────────────────────────────────────────
	if err := os.WriteFile(tomlPath, []byte(config.WorkspaceTOML), 0o644); err != nil {
		return fmt.Errorf("write loom.toml: %w", err)
	}
	fmt.Println("  created  loom.toml")

	// ── loom/src/.export.loom ───────────────────────────────────────────────
	if err := export.WriteDefault(filepath.Join(loomDir, "src", export.Filename)); err != nil {
		return fmt.Errorf("write .export.loom: %w", err)
	}
	fmt.Println("  created  loom/src/.export.loom")

	// ── loom/src/.dependency.loom ───────────────────────────────────────────
	if err := deps.WriteDefault(filepath.Join(loomDir, "src", deps.Filename)); err != nil {
		return fmt.Errorf("write .dependency.loom: %w", err)
	}
	fmt.Println("  created  loom/src/.dependency.loom")

	// ── loom/context/REPO.md ────────────────────────────────────────────────
	repoMD := filepath.Join(loomDir, "context", "REPO.md")
	if err := os.WriteFile(repoMD, []byte(repoMDTemplate), 0o644); err != nil {
		return fmt.Errorf("write REPO.md: %w", err)
	}
	fmt.Println("  created  loom/context/REPO.md")

	// ── loom/context/TODO.md ────────────────────────────────────────────────
	todoMD := filepath.Join(loomDir, "context", "TODO.md")
	if err := os.WriteFile(todoMD, []byte(todoMDTemplate), 0o644); err != nil {
		return fmt.Errorf("write TODO.md: %w", err)
	}
	fmt.Println("  created  loom/context/TODO.md")

	// ── loom/.loom.env ──────────────────────────────────────────────────────
	loomEnvPath := filepath.Join(loomDir, ".loom.env")
	if err := os.WriteFile(loomEnvPath, []byte(loomEnvTemplate), 0o644); err != nil {
		return fmt.Errorf("write .loom.env: %w", err)
	}
	fmt.Println("  created  loom/.loom.env")

	// ── loom/.loom.config ───────────────────────────────────────────────────
	loomCfgPath := filepath.Join(loomDir, ".loom.config")
	if _, err := os.Stat(loomCfgPath); os.IsNotExist(err) {
		// Write a default loomlocker config using loomlocker's own function.
		// We write it manually here to avoid importing the loomlocker module.
		if err := os.WriteFile(loomCfgPath, []byte(defaultLoomConfig), 0o644); err != nil {
			return fmt.Errorf("write .loom.config: %w", err)
		}
		fmt.Println("  created  loom/.loom.config")
	}

	// ── loom/.loom.secret ───────────────────────────────────────────────────
	secretPath := filepath.Join(loomDir, secret.Filename)
	if _, err := os.Stat(secretPath); os.IsNotExist(err) {
		if err := os.WriteFile(secretPath, []byte(secret.TemplateContent), 0o600); err != nil {
			return fmt.Errorf("write .loom.secret: %w", err)
		}
		fmt.Println("  created  loom/.loom.secret  (add your API keys here — never commit)")
	}

	// ── .gitignore entries ──────────────────────────────────────────────────
	gitignorePath := filepath.Join(cwd, ".gitignore")
	for _, entry := range []string{"loom/.loom.secret", "loom/.loom.config", "loom/loompack/"} {
		appendGitignoreEntry(gitignorePath, entry)
	}
	fmt.Println("  updated  .gitignore")

	// ── Sample files ────────────────────────────────────────────────────────
	if initSample {
		if err := writeSampleFiles(filepath.Join(loomDir, "src")); err != nil {
			return err
		}
	}

	fmt.Printf("\nProject initialized. Next steps:\n")
	fmt.Printf("  • Edit loom/context/REPO.md with your project architecture\n")
	fmt.Printf("  • Edit loom/context/TODO.md with current tasks\n")
	fmt.Printf("  • Add your API key to loom/.loom.secret\n")
	fmt.Printf("  • Run `loom start` to generate a prompt pack\n")
	fmt.Printf("  • Run `loom inspect` to validate prompts\n")
	return nil
}

// appendGitignoreEntry adds entry to .gitignore if it is not already present.
func appendGitignoreEntry(path, entry string) {
	data, _ := os.ReadFile(path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	prefix := ""
	if len(data) > 0 && data[len(data)-1] != '\n' {
		prefix = "\n"
	}
	_, _ = f.WriteString(prefix + entry + "\n")
}

func writeSampleFiles(srcDir string) error {
	samples := map[string]string{
		"prompts/BaseEngineer.prompt.loom": `prompt BaseEngineer {
  summary:
    General-purpose engineering assistant.

  persona:
    You are a senior software engineer who writes clear, maintainable, production-ready code.

  objective:
    Help the user solve software engineering tasks with correctness, clarity, and practical judgment.

  constraints:
    - Do not hallucinate APIs.
    - Ask for missing information only when necessary.
    - Prefer simple solutions before complex ones.

  format:
    - Summary
    - Analysis
    - Recommendation
}
`,
		"prompts/CodeReviewer.prompt.loom": `prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review the provided code for correctness, maintainability, readability, and production readiness.

  instructions +=
    - Read the code carefully.
    - Identify correctness issues.
    - Suggest practical improvements.

  format :=
    - Summary
    - Issues Found
    - Suggested Fixes
    - Final Recommendation
}
`,
		"blocks/Conventions.block.loom": `block Conventions {
  constraints:
    - Follow the language's official style guide.
    - Return errors explicitly; never swallow them silently.
    - Write tests alongside new code.
}
`,
		"prompts/TestWriter.prompt.loom": `prompt TestWriter inherits BaseEngineer {
  use Conventions

  objective :=
    Generate useful, comprehensive tests for the provided code.

  instructions +=
    - Identify the behavior that needs to be tested.
    - Cover success cases, failure cases, and edge cases.
    - Prefer readable tests over overly clever tests.

  format :=
    - Test Strategy
    - Test Cases
    - Generated Test Code
}
`,
	}

	for rel, content := range samples {
		full := filepath.Join(srcDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", rel, err)
		}
		fmt.Printf("  created  loom/src/%s\n", rel)
	}
	return nil
}

// ── Templates ────────────────────────────────────────────────────────────────

const repoMDTemplate = `# Repository Architecture

> Fill this file with notes about your project that should inform AI-generated prompts.
> loom start reads this file to tailor prompt packs to your codebase.

## Overview

<!-- What does this project do? What problem does it solve? -->

## Tech Stack

<!-- Languages, frameworks, databases, infra. Example:
- Language: Go 1.22
- Framework: net/http + Cobra
- Database: PostgreSQL 16 (pgx/v5)
- Deploy: Docker + fly.io
-->

## Key Directories

<!-- Brief description of important directories and what lives there. -->

## Testing Approach

<!-- How tests are run, what frameworks are used, naming conventions. -->

## Important Conventions

<!-- Coding conventions, PR practices, naming rules specific to this repo. -->
`

const todoMDTemplate = `# Current Tasks

> List what you're currently working on or planning to implement.
> loom start reads this file to generate task-specific prompts.

## In Progress

- [ ] (add your current tasks here)

## Upcoming

- [ ] (add upcoming tasks here)

## Notes

<!-- Any context that helps understand the current state of work -->
`

const loomEnvTemplate = `# .loom.env — environment overrides for loom commands
# These are loaded automatically by loom. Do not commit secrets here.
# Use loom/.loom.secret for API keys.

# LOOM_REGISTRY_URL=https://registry.promptloom.dev
# LOOM_HOST=http://localhost
`

const defaultLoomConfig = `{
  "secret": ["loom/.loom.secret"],
  "ignore": [],
  "permission": {
    "read": ["*"],
    "write": ["*"]
  },
  "loomlocker": {
    "active": true,
    "lockhost": "http://localhost",
    "port": "8053",
    "recoverable": false,
    "unlock_duration_seconds": 10
  },
  "custom": {}
}
`
