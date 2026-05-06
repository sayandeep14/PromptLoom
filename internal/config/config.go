package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Project struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

type Paths struct {
	Prompts  string `toml:"prompts"`
	Blocks   string `toml:"blocks"`
	Overlays string `toml:"overlays"`
	Out      string `toml:"out"`
}

type Render struct {
	DefaultFormat      string `toml:"default_format"`
	IncludeMetadata    bool   `toml:"include_metadata"`
	IncludeSourceMap   bool   `toml:"include_source_map"`
	IncludeSourceMapV2 bool   `toml:"include_sourcemap"`
	IncludeFingerprint bool   `toml:"include_fingerprint"`
}

type Target struct {
	Prompt string `toml:"prompt"`
	Format string `toml:"format"`
	Dest   string `toml:"dest"`
}

type Validation struct {
	RequireObjective      bool `toml:"require_objective"`
	RequireFormat         bool `toml:"require_format"`
	RequireContract       bool `toml:"require_contract"`
	WarnOnEmptyContext    bool `toml:"warn_on_empty_context"`
	WarnOnDeepInheritance bool `toml:"warn_on_deep_inheritance"`
	MaxInheritanceDepth   int  `toml:"max_inheritance_depth"`
	SmellConstraintLimit  int  `toml:"smell_constraint_limit"`
	TokenLimitWarn        int  `toml:"token_limit_warn"`
}

type Testing struct {
	Provider    string `toml:"provider"`     // "gemini" or "anthropic"
	APIKeyEnv   string `toml:"api_key_env"`
	DefaultModel string `toml:"default_model"`
	TimeoutSec  int    `toml:"timeout_sec"`
}

type Config struct {
	Project    Project                      `toml:"project"`
	Paths      Paths                        `toml:"paths"`
	Render     Render                       `toml:"render"`
	Validation Validation                   `toml:"validation"`
	Testing    Testing                      `toml:"testing"`
	Profiles   map[string]map[string]string `toml:"profile"`
	Targets    []Target                     `toml:"targets"`
}

func Defaults() *Config {
	return &Config{
		Project: Project{
			Name:    "my-prompts",
			Version: "0.1.0",
		},
		Paths: Paths{
			Prompts:  "prompts",
			Blocks:   "blocks",
			Overlays: "overlays",
			Out:      "dist/prompts",
		},
		Render: Render{
			DefaultFormat:      "markdown",
			IncludeMetadata:    false,
			IncludeSourceMap:   false,
			IncludeFingerprint: false,
		},
		Testing: Testing{
			Provider:     "gemini",
			APIKeyEnv:    "GEMINI_API_KEY",
			DefaultModel: "gemini-2.5-flash",
			TimeoutSec:   30,
		},
		Validation: Validation{
			RequireObjective:      true,
			RequireFormat:         true,
			RequireContract:       false,
			WarnOnEmptyContext:    true,
			WarnOnDeepInheritance: true,
			MaxInheritanceDepth:   3,
			SmellConstraintLimit:  25,
			TokenLimitWarn:        0,
		},
	}
}

func Load(dir string) (*Config, error) {
	path := filepath.Join(dir, "loom.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read loom.toml: %w", err)
	}
	cfg := Defaults()
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return nil, fmt.Errorf("could not parse loom.toml: %w", err)
	}
	if cfg.Render.IncludeSourceMapV2 {
		cfg.Render.IncludeSourceMap = true
	}
	if cfg.Render.DefaultFormat == "" {
		cfg.Render.DefaultFormat = "markdown"
	}
	return cfg, nil
}

// FindProjectRoot resolves the most likely PromptLoom project directory for start.
// It first searches upward for loom.toml.
// If none is found, it supports the repo-root developer workflow by checking for a
// single project under examples/*/loom.toml.
func FindProjectRoot(start string) (string, bool) {
	start, err := filepath.Abs(start)
	if err != nil {
		return start, false
	}

	for dir := start; ; dir = filepath.Dir(dir) {
		if fileExists(filepath.Join(dir, "loom.toml")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	matches, err := filepath.Glob(filepath.Join(start, "examples", "*", "loom.toml"))
	if err != nil {
		return start, false
	}

	if len(matches) == 1 {
		return filepath.Dir(matches[0]), true
	}
	return start, false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

const DefaultTOML = `[project]
name = "my-prompts"
version = "0.1.0"

[paths]
prompts  = "prompts"
blocks   = "blocks"
overlays = "overlays"
out      = "dist/prompts"

[render]
default_format      = "markdown"
include_metadata    = false
include_sourcemap   = false
include_fingerprint = false

[validation]
require_objective        = true
require_format           = true
require_contract         = false
warn_on_empty_context    = true
warn_on_deep_inheritance = true
max_inheritance_depth    = 3
smell_constraint_limit   = 25
token_limit_warn         = 0

[testing]
provider      = "gemini"
api_key_env   = "GEMINI_API_KEY"
default_model = "gemini-2.5-flash"
timeout_sec   = 30
`
