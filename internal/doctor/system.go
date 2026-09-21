package doctor

import (
	"fmt"
	"path/filepath"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/loader"
)

// Level is how serious a system check outcome is.
type Level int

const (
	LevelOK   Level = iota // working
	LevelWarn              // something optional is missing; the message says what it affects
	LevelFail              // the installation or project is broken
)

// SysCheck is one result of the install self-check.
type SysCheck struct {
	Name   string
	Level  Level
	Detail string
	Hint   string // what to do (or what the missing piece is needed for)
}

// Env is everything System looks at, injectable for tests.
type Env struct {
	Version     string
	Dir         string
	Getenv      func(string) string
	LookPath    func(string) (string, error)
	RegistryURL func(dir string) (url, source string)
}

// System checks that loom itself is set up: version, the project, and the optional tools the
// commands lean on (git, a model API key, a registry, loomlocker). A missing optional piece is a
// warning that says which commands need it; only a broken project is a failure.
func System(e Env) []SysCheck {
	var out []SysCheck
	add := func(name string, l Level, detail, hint string) {
		out = append(out, SysCheck{Name: name, Level: l, Detail: detail, Hint: hint})
	}

	add("loom", LevelOK, e.Version, "")

	// project
	var cfg *config.Config
	if root, ok := config.FindProjectRoot(e.Dir); !ok {
		add("project", LevelWarn, "no loom.toml here or in a parent directory", "run `loom init` to create a project")
	} else {
		c, err := config.Load(root)
		if err != nil {
			add("project", LevelFail, err.Error(), "fix loom.toml")
		} else {
			cfg = c
			reg, _, err := loader.Load(root)
			if err != nil {
				add("project", LevelFail, err.Error(), "run `loom inspect` for the full list of problems")
			} else {
				add("project", LevelOK, fmt.Sprintf("%d prompts, %d blocks, %d overlays (%s)",
					reg.PromptCount(), reg.BlockCount(), reg.OverlayCount(), displayPath(e.Dir, root)), "")
			}
		}
	}

	// git
	if p, err := e.LookPath("git"); err == nil {
		add("git", LevelOK, p, "")
	} else {
		add("git", LevelWarn, "not found on PATH", "needed by `loom blame`, `loom changelog`, `--with git:diff` and `loom review`")
	}

	// model API key
	provider, model, envVar := "gemini", "", ""
	if cfg != nil {
		provider, model, envVar = cfg.Testing.Provider, cfg.Testing.DefaultModel, cfg.Testing.APIKeyEnv
	}
	if envVar == "" {
		envVar, _, _ = config.ProviderDefaults(provider)
	}
	if e.Getenv(envVar) != "" {
		detail := "$" + envVar + " is set"
		if model != "" {
			detail += " (" + model + ")"
		}
		add("model API key", LevelOK, detail, "")
	} else {
		add("model API key", LevelWarn, "$"+envVar+" is not set",
			"only `loom test`, `loom summarize` and `loom start` call a model; put the key in .loomsecret or export it")
	}

	// registry
	if url, source := e.RegistryURL(e.Dir); url != "" {
		add("registry", LevelOK, url+" ("+source+")", "")
	} else {
		add("registry", LevelWarn, "not configured", "only `loom install` and `loom publish` need one: set LOOM_REGISTRY_URL or [registry] url in loom.toml")
	}

	// loomlocker
	if p, err := e.LookPath("loomlocker"); err == nil {
		add("loomlocker", LevelOK, p, "")
	} else {
		add("loomlocker", LevelWarn, "not found on PATH", "optional: only `loom execute --unlock` uses it")
	}
	return out
}

// Failed reports whether any check is a failure.
func Failed(checks []SysCheck) bool {
	for _, c := range checks {
		if c.Level == LevelFail {
			return true
		}
	}
	return false
}

func displayPath(cwd, root string) string {
	if rel, err := filepath.Rel(cwd, root); err == nil && rel != "." {
		return rel
	}
	return "current directory"
}
