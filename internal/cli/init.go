package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/spf13/cobra"
)

var initSample bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new PromptLoom project",
	RunE:  runInit,
}

func init() {
	initCmd.Flags().BoolVar(&initSample, "sample", false, "create sample prompts and blocks")
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	tomlPath := filepath.Join(cwd, "loom.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		return fmt.Errorf("loom.toml already exists in this directory")
	}

	if err := os.WriteFile(tomlPath, []byte(config.DefaultTOML), 0644); err != nil {
		return fmt.Errorf("could not write loom.toml: %w", err)
	}
	fmt.Println("  created  loom.toml")

	for _, dir := range []string{"prompts", "blocks", "overlays", "contexts", "dist/prompts"} {
		full := filepath.Join(cwd, dir)
		if err := os.MkdirAll(full, 0755); err != nil {
			return fmt.Errorf("could not create %s: %w", dir, err)
		}
		fmt.Printf("  created  %s/\n", dir)
	}

	if initSample {
		if err := writeSampleFiles(cwd); err != nil {
			return err
		}
	}

	fmt.Println("\nProject initialized. Run `loom inspect` to validate your prompts.")
	return nil
}

func writeSampleFiles(root string) error {
	samples := map[string]string{
		"prompts/base.prompt": `prompt BaseEngineer {
  summary:
    General-purpose engineering assistant prompt.

  persona:
    You are a senior software engineer who writes clear, maintainable, production-ready code.

  objective:
    Help the user solve software engineering tasks with correctness, clarity, and practical judgment.

  constraints:
    - Do not hallucinate APIs.
    - Ask for missing information only when necessary.
    - Prefer simple solutions before complex ones.
    - Explain important trade-offs.

  format:
    - Summary
    - Analysis
    - Recommendation
}
`,
		"prompts/code-review.prompt": `prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review the provided code for correctness, maintainability, readability, and production readiness.

  instructions +=
    - Read the code carefully.
    - Identify correctness issues.
    - Identify maintainability issues.
    - Suggest practical improvements.

  format :=
    - Summary
    - Issues Found
    - Suggested Fixes
    - Final Recommendation
}
`,
		"blocks/spring-boot-rules.prompt": `block SpringBootRules {
  constraints:
    - Check controller, service, and repository separation.
    - Check transaction boundaries.
    - Check JPA entity mappings.
    - Check exception handling.
    - Check retry and timeout behavior for external calls.
}
`,
		"prompts/spring-boot-review.prompt": `prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules

  context:
    The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging.

  objective :=
    Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness.
}
`,
		"prompts/test-writer.prompt": `prompt TestWriter inherits BaseEngineer {
  objective :=
    Generate useful tests for the provided code.

  instructions +=
    - Identify the behavior that needs to be tested.
    - Cover success cases, failure cases, and edge cases.
    - Prefer readable tests over overly clever tests.

  constraints +=
    - Do not change production code unless necessary.
    - Use the testing framework appropriate for the project.

  format :=
    - Test Strategy
    - Test Cases
    - Generated Test Code
    - Notes
}
`,
	}

	for rel, content := range samples {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			return fmt.Errorf("could not write %s: %w", rel, err)
		}
		fmt.Printf("  created  %s\n", rel)
	}
	return nil
}
