package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestDetectStack(t *testing.T) {
	cases := []struct {
		name                          string
		files                         map[string]string
		stack                         Stack
		lang, framework, build, tests string
	}{
		{"go", map[string]string{"go.mod": "module x\n"}, StackGo, "Go", "", "Go modules", "go test"},
		{"go gin + make", map[string]string{"go.mod": "require github.com/gin-gonic/gin v1", "Makefile": ""}, StackGo, "Go", "Gin", "Make + Go modules", "go test"},
		{"go echo", map[string]string{"go.mod": "require github.com/labstack/echo/v4 v4"}, StackGo, "Go", "Echo", "Go modules", "go test"},
		{"go grpc", map[string]string{"go.mod": "require google.golang.org/grpc v1"}, StackGo, "Go", "gRPC", "Go modules", "go test"},
		{"rust", map[string]string{"Cargo.toml": ""}, StackRust, "Rust", "", "Cargo", "cargo test"},
		{"python pip", map[string]string{"requirements.txt": "Flask==2\n"}, StackPython, "Python", "Flask", "pip", ""},
		{"python fastapi pytest", map[string]string{"pyproject.toml": "dependencies=['FastAPI','pytest']"}, StackPython, "Python", "FastAPI", "pyproject (PEP 517)", "pytest"},
		{"python django", map[string]string{"requirements.txt": "django"}, StackPython, "Python", "Django", "pip", ""},
		{"python conftest", map[string]string{"setup.py": "", "conftest.py": ""}, StackPython, "Python", "", "pip", "pytest"},
		{"maven", map[string]string{"pom.xml": "<project/>"}, StackJava, "Java", "", "Maven", "JUnit"},
		{"spring maven", map[string]string{"pom.xml": "<artifactId>spring-boot-starter</artifactId>"}, StackJavaSpring, "Java", "Spring Boot", "Maven", "JUnit"},
		{"gradle kts spring", map[string]string{"build.gradle.kts": "id(\"org.springframework.boot\")\nspring-boot"}, StackJavaSpring, "Java/Kotlin", "Spring Boot", "Gradle", "JUnit"},
		{"gradle", map[string]string{"build.gradle": "plugins {}"}, StackJava, "Java/Kotlin", "", "Gradle", "JUnit"},
		{"js", map[string]string{"package.json": `{"dependencies":{"express":"4"}}`}, StackJavaScript, "JavaScript", "Express", "npm/yarn/pnpm", ""},
		{"ts by tsconfig", map[string]string{"package.json": "{}", "tsconfig.json": "{}"}, StackTypeScript, "TypeScript", "", "npm/yarn/pnpm", ""},
		{"ts next vitest", map[string]string{"package.json": `{"devDependencies":{"typescript":"5","vitest":"1"},"dependencies":{"next":"14","react":"18"}}`}, StackTypeScript, "TypeScript", "Next.js", "npm/yarn/pnpm", "Vitest"},
		{"react jest", map[string]string{"package.json": `{"dependencies":{"react":"18"},"devDependencies":{"jest":"29"}}`}, StackJavaScript, "JavaScript", "React", "npm/yarn/pnpm", "Jest"},
		{"vue", map[string]string{"package.json": `{"dependencies":{"vue":"3"}}`}, StackJavaScript, "JavaScript", "Vue", "npm/yarn/pnpm", ""},
		{"nest", map[string]string{"package.json": `{"dependencies":{"@nestjs/core":"10"}}`}, StackJavaScript, "JavaScript", "NestJS", "npm/yarn/pnpm", ""},
		{"docker go", map[string]string{"Dockerfile": "FROM golang:1.22\n"}, StackUnknown, "Go", "", "", ""},
		{"docker python", map[string]string{"Dockerfile": "FROM python:3.12\n"}, StackUnknown, "Python", "", "", ""},
		{"docker node", map[string]string{"Dockerfile": "FROM node:20\n"}, StackUnknown, "Node.js", "", "", ""},
		{"empty", map[string]string{}, StackUnknown, "", "", "", ""},
	}
	for _, c := range cases {
		info, err := Scan(project(t, c.files))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if info.Stack != c.stack || info.Language != c.lang || info.Framework != c.framework || info.BuildTool != c.build || info.TestFramework != c.tests {
			t.Errorf("%s: got %+v", c.name, info)
		}
	}
}

func TestContextFilesNewLayoutWinsOverLegacy(t *testing.T) {
	dir := project(t, map[string]string{
		"CLAUDE.md": "legacy", "TODO.md": "legacy todo",
		"loom/context/REPO.md": "new repo", "loom/context/TODO.md": "new todo",
	})
	info, _ := Scan(dir)
	if info.ClaudeMD != "new repo" || info.TodoMD != "new todo" || !info.HasClaudeMD || !info.HasTodoMD {
		t.Errorf("%+v", info)
	}
	info, _ = Scan(project(t, map[string]string{"CLAUDE.md": "only legacy"}))
	if info.ClaudeMD != "only legacy" || info.HasTodoMD || info.TodoMD != "" {
		t.Errorf("%+v", info)
	}
	info, _ = Scan(project(t, nil))
	if info.HasClaudeMD || info.ClaudeMD != "" {
		t.Errorf("%+v", info)
	}
}

func TestContextFilesAreCapped(t *testing.T) {
	big := strings.Repeat("x", maxContextFile+5000)
	info, _ := Scan(project(t, map[string]string{"CLAUDE.md": big}))
	if len(info.ClaudeMD) > maxContextFile+100 || !strings.Contains(info.ClaudeMD, "[truncated") {
		t.Errorf("len=%d", len(info.ClaudeMD))
	}
	exact := strings.Repeat("y", maxContextFile)
	info, _ = Scan(project(t, map[string]string{"CLAUDE.md": exact}))
	if info.ClaudeMD != exact {
		t.Error("a file of exactly the limit is kept whole")
	}
}

func TestScanRejectsBadDirectories(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("a missing directory must be an error, not an empty project")
	}
	f := filepath.Join(t.TempDir(), "file")
	os.WriteFile(f, nil, 0o644)
	if _, err := Scan(f); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("%v", err)
	}
	info, err := Scan(".")
	if err != nil || !filepath.IsAbs(info.Dir) {
		t.Errorf("relative dir is made absolute: %v %v", info, err)
	}
}

func TestExistingAITools(t *testing.T) {
	dir := project(t, map[string]string{
		"CLAUDE.md": "", ".cursor/rules": "", ".github/copilot-instructions.md": "", ".github/copilot_instructions.md": "", ".aider.conf.yml": "",
	})
	info, _ := Scan(dir)
	if strings.Join(info.ExistingAITools, ",") != "CLAUDE.md,.cursor/,copilot-instructions.md,aider" {
		t.Errorf("%v (copilot must be listed once)", info.ExistingAITools)
	}
	info, _ = Scan(project(t, nil))
	if len(info.ExistingAITools) != 0 {
		t.Errorf("%v", info.ExistingAITools)
	}
}

func TestSummary(t *testing.T) {
	info := &Info{Language: "Go", Framework: "Gin", BuildTool: "Make", TestFramework: "go test", ExistingAITools: []string{"CLAUDE.md", "aider"}}
	s := info.Summary()
	for _, want := range []string{"Language:   Go", "Framework:  Gin", "Build:      Make", "Tests:      go test", "Existing:   CLAUDE.md, aider"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if s := (&Info{}).Summary(); s != "  Language:   unknown\n" {
		t.Errorf("%q", s)
	}
}
