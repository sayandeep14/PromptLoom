// Package workspace scans the developer's project directory to detect tech stack,
// read AI context files (CLAUDE.md, TODO.md), and summarise what was found.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Stack is a detected tech stack identifier.
type Stack string

const (
	StackGo         Stack = "go"
	StackTypeScript Stack = "typescript"
	StackJavaScript Stack = "javascript"
	StackPython     Stack = "python"
	StackRust       Stack = "rust"
	StackJavaSpring Stack = "java-spring"
	StackJava       Stack = "java"
	StackUnknown    Stack = "unknown"
)

// Info holds everything workspace scanning discovers about the project.
type Info struct {
	// Detected tech
	Stack         Stack
	Language      string
	Framework     string
	BuildTool     string
	TestFramework string
	ExistingAITools []string // e.g., "CLAUDE.md", ".cursor/", "copilot-instructions.md"

	// Context files
	ClaudeMD    string // full content of CLAUDE.md (empty if absent)
	TodoMD      string // full content of TODO.md (empty if absent)
	HasTodoMD   bool
	HasClaudeMD bool

	// Project dir
	Dir string
}

// Scan reads dir and returns a populated Info struct.
// It returns an error only if dir is not readable.
func Scan(dir string) (*Info, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	info := &Info{Dir: abs}

	info.ClaudeMD, info.HasClaudeMD = readFile(abs, "CLAUDE.md")
	info.TodoMD, info.HasTodoMD = readFile(abs, "TODO.md")

	detectStack(abs, info)
	detectAITools(abs, info)

	return info, nil
}

// Summary returns a short human-readable description for display.
func (i *Info) Summary() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "  Language:   %s\n", orUnknown(i.Language))
	if i.Framework != "" {
		fmt.Fprintf(&sb, "  Framework:  %s\n", i.Framework)
	}
	if i.BuildTool != "" {
		fmt.Fprintf(&sb, "  Build:      %s\n", i.BuildTool)
	}
	if i.TestFramework != "" {
		fmt.Fprintf(&sb, "  Tests:      %s\n", i.TestFramework)
	}
	if len(i.ExistingAITools) > 0 {
		fmt.Fprintf(&sb, "  Existing:   %s\n", strings.Join(i.ExistingAITools, ", "))
	}
	return sb.String()
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func detectStack(dir string, info *Info) {
	switch {
	case fileExists(dir, "go.mod"):
		info.Stack = StackGo
		info.Language = "Go"
		info.BuildTool = "Go modules"
		if fileContains(dir, "go.mod", "github.com/gin-gonic") {
			info.Framework = "Gin"
		} else if fileContains(dir, "go.mod", "github.com/labstack/echo") {
			info.Framework = "Echo"
		} else if fileContains(dir, "go.mod", "google.golang.org/grpc") {
			info.Framework = "gRPC"
		}
		if fileExists(dir, "Makefile") {
			info.BuildTool = "Make + Go modules"
		}
		info.TestFramework = "go test"

	case fileExists(dir, "Cargo.toml"):
		info.Stack = StackRust
		info.Language = "Rust"
		info.BuildTool = "Cargo"
		info.TestFramework = "cargo test"

	case fileExists(dir, "pyproject.toml") || fileExists(dir, "requirements.txt") || fileExists(dir, "setup.py"):
		info.Stack = StackPython
		info.Language = "Python"
		if fileExists(dir, "pyproject.toml") {
			info.BuildTool = "pyproject (PEP 517)"
		} else {
			info.BuildTool = "pip"
		}
		if fileContains(dir, "pyproject.toml", "fastapi") || fileContains(dir, "requirements.txt", "fastapi") {
			info.Framework = "FastAPI"
		} else if fileContains(dir, "pyproject.toml", "django") || fileContains(dir, "requirements.txt", "django") {
			info.Framework = "Django"
		} else if fileContains(dir, "pyproject.toml", "flask") || fileContains(dir, "requirements.txt", "flask") {
			info.Framework = "Flask"
		}
		if fileContains(dir, "pyproject.toml", "pytest") || fileExists(dir, "pytest.ini") || fileExists(dir, "conftest.py") {
			info.TestFramework = "pytest"
		}

	case fileExists(dir, "pom.xml"):
		info.Language = "Java"
		info.BuildTool = "Maven"
		info.TestFramework = "JUnit"
		if fileContains(dir, "pom.xml", "spring-boot") {
			info.Stack = StackJavaSpring
			info.Framework = "Spring Boot"
		} else {
			info.Stack = StackJava
		}

	case fileExists(dir, "build.gradle") || fileExists(dir, "build.gradle.kts"):
		info.Language = "Java/Kotlin"
		info.BuildTool = "Gradle"
		info.TestFramework = "JUnit"
		if fileContains(dir, "build.gradle", "spring-boot") || fileContains(dir, "build.gradle.kts", "spring-boot") {
			info.Stack = StackJavaSpring
			info.Framework = "Spring Boot"
		} else {
			info.Stack = StackJava
		}

	case fileExists(dir, "package.json"):
		if fileExists(dir, "tsconfig.json") || fileContains(dir, "package.json", `"typescript"`) {
			info.Stack = StackTypeScript
			info.Language = "TypeScript"
		} else {
			info.Stack = StackJavaScript
			info.Language = "JavaScript"
		}
		info.BuildTool = "npm/yarn/pnpm"
		if fileContains(dir, "package.json", `"next"`) {
			info.Framework = "Next.js"
		} else if fileContains(dir, "package.json", `"react"`) {
			info.Framework = "React"
		} else if fileContains(dir, "package.json", `"vue"`) {
			info.Framework = "Vue"
		} else if fileContains(dir, "package.json", `"express"`) {
			info.Framework = "Express"
		} else if fileContains(dir, "package.json", `"nestjs"`) || fileContains(dir, "package.json", `"@nestjs"`) {
			info.Framework = "NestJS"
		}
		if fileContains(dir, "package.json", `"jest"`) {
			info.TestFramework = "Jest"
		} else if fileContains(dir, "package.json", `"vitest"`) {
			info.TestFramework = "Vitest"
		}

	default:
		info.Stack = StackUnknown
		// Try to infer language from Dockerfile
		if fileContains(dir, "Dockerfile", "FROM golang") || fileContains(dir, "Dockerfile", "FROM go:") {
			info.Language = "Go"
		} else if fileContains(dir, "Dockerfile", "FROM python") || fileContains(dir, "Dockerfile", "FROM node") {
			info.Language = strings.Fields(func() string {
				if fileContains(dir, "Dockerfile", "FROM python") {
					return "Python"
				}
				return "Node.js"
			}())[0]
		}
	}
}

func detectAITools(dir string, info *Info) {
	checks := []struct {
		path  string
		label string
	}{
		{"CLAUDE.md", "CLAUDE.md"},
		{".cursor", ".cursor/"},
		{".github/copilot-instructions.md", "copilot-instructions.md"},
		{".github/copilot_instructions.md", "copilot-instructions.md"},
		{".aider.conf.yml", "aider"},
		{".codeium", "codeium"},
	}
	seen := map[string]bool{}
	for _, c := range checks {
		if _, err := os.Stat(filepath.Join(dir, c.path)); err == nil {
			if !seen[c.label] {
				info.ExistingAITools = append(info.ExistingAITools, c.label)
				seen[c.label] = true
			}
		}
	}
}

func readFile(dir, name string) (string, bool) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func fileContains(dir, name, substr string) bool {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), strings.ToLower(substr))
}
