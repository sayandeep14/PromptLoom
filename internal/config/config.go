package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Provider     string `toml:"provider"` // "gemini", "anthropic" or "openai"
	APIKeyEnv    string `toml:"api_key_env"`
	DefaultModel string `toml:"default_model"`
	TimeoutSec   int    `toml:"timeout_sec"`
}

type Registry struct {
	URL string `toml:"url"` // base URL of the pack registry (optional)
}

// Price is what one model costs, in USD per million tokens — supplied by the project, never
// guessed by loom: real rates vary by contract and change over time, so `loom usage`/`loom bench`
// show token counts only for any model without a matching entry here, rather than an invented
// number. See internal/usage.
type Price struct {
	Provider string  `toml:"provider"`
	Model    string  `toml:"model"`
	Input    float64 `toml:"input_per_million"`
	Output   float64 `toml:"output_per_million"`
}

type Config struct {
	Project    Project                      `toml:"project"`
	Paths      Paths                        `toml:"paths"`
	Render     Render                       `toml:"render"`
	Validation Validation                   `toml:"validation"`
	Testing    Testing                      `toml:"testing"`
	Profiles   map[string]map[string]string `toml:"profile"`
	Targets    []Target                     `toml:"targets"`
	Registry   Registry                     `toml:"registry"`
	Pricing    []Price                      `toml:"pricing"`
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
	md, err := toml.Decode(string(data), cfg)
	if err != nil {
		return nil, fmt.Errorf("could not parse loom.toml: %w", err)
	}
	// The defaults describe the default provider (Gemini). A project that picks another provider
	// and does not name its own key variable or model gets THAT provider's defaults: otherwise
	// `provider = "anthropic"` would look for $GEMINI_API_KEY and send a Gemini model name to
	// Anthropic.
	if envVar, model, ok := ProviderDefaults(cfg.Testing.Provider); ok && !strings.EqualFold(cfg.Testing.Provider, "gemini") {
		if !md.IsDefined("testing", "api_key_env") {
			cfg.Testing.APIKeyEnv = envVar
		}
		if !md.IsDefined("testing", "default_model") {
			cfg.Testing.DefaultModel = model
		}
	}
	if cfg.Render.IncludeSourceMapV2 {
		cfg.Render.IncludeSourceMap = true
	}
	if cfg.Render.DefaultFormat == "" {
		cfg.Render.DefaultFormat = "markdown"
	}
	return cfg, nil
}

// ProviderDefaults returns the environment variable that holds the API key and the default model
// of a model provider ("gemini", "anthropic" or "openai", case-insensitive). ok is false for an
// unknown provider.
func ProviderDefaults(provider string) (envVar, model string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "gemini":
		return "GEMINI_API_KEY", "gemini-2.5-flash", true
	case "anthropic":
		return "ANTHROPIC_API_KEY", "claude-sonnet-4-6", true
	case "openai":
		return "OPENAI_API_KEY", "gpt-4o-mini", true
	}
	return "", "", false
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

// WorkspaceTOML is the loom.toml written by `loom init` for the new loom/ workspace structure.
const WorkspaceTOML = `[project]
name = "my-project"
version = "0.1.0"

[paths]
prompts  = "loom/src/prompts"
blocks   = "loom/src/blocks"
overlays = "loom/src/overlays"
out      = "loom/dist"

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

// SourceDirs are the absolute directories where the project at dir keeps its prompt, block and
// overlay files: [paths] from loom.toml, or "prompts", "blocks" and "overlays" when there is no
// (readable) config. Commands that create or move source files use them so the files land where
// `loom inspect` looks.
type SourceDirs struct {
	Prompts, Blocks, Overlays string
}

// Dirs returns the SourceDirs of the project at dir.
func Dirs(dir string) SourceDirs {
	prompts, blocks, overlays := "prompts", "blocks", "overlays"
	if cfg, err := Load(dir); err == nil {
		if cfg.Paths.Prompts != "" {
			prompts = cfg.Paths.Prompts
		}
		if cfg.Paths.Blocks != "" {
			blocks = cfg.Paths.Blocks
		}
		if cfg.Paths.Overlays != "" {
			overlays = cfg.Paths.Overlays
		}
	}
	abs := func(rel string) string {
		if filepath.IsAbs(rel) {
			return rel
		}
		return filepath.Join(dir, rel)
	}
	return SourceDirs{Prompts: abs(prompts), Blocks: abs(blocks), Overlays: abs(overlays)}
}

// PromptsDir returns the absolute directory where the project at dir keeps its .prompt.loom
// files (see Dirs).
func PromptsDir(dir string) string { return Dirs(dir).Prompts }
