package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sysToml = "[project]\nname = \"t\"\nversion = \"0\"\n[paths]\nprompts = \"prompts\"\nblocks = \"blocks\"\noverlays = \"overlays\"\nout = \"dist\"\n"

func sysProject(t *testing.T, extraToml string, prompts map[string]string) string {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(sysToml+extraToml), 0o644)
	for name, body := range prompts {
		os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
		os.WriteFile(filepath.Join(dir, "prompts", name), []byte(body), 0o644)
	}
	return dir
}

func env(dir string, vars map[string]string, tools ...string) Env {
	have := map[string]bool{}
	for _, t := range tools {
		have[t] = true
	}
	return Env{
		Version: "v9.9.9", Dir: dir,
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(n string) (string, error) {
			if have[n] {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("not found")
		},
		RegistryURL: func(string) (string, string) { return vars["REG"], "test" },
	}
}

func byName(cs []SysCheck) map[string]SysCheck {
	m := map[string]SysCheck{}
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

func TestSystemAllGood(t *testing.T) {
	dir := sysProject(t, "", map[string]string{"A.prompt.loom": "prompt A {\n  persona :=\n    p\n}\n"})
	cs := System(env(dir, map[string]string{"GEMINI_API_KEY": "k", "REG": "https://r.example"}, "git", "loomlocker"))
	m := byName(cs)
	for name, c := range m {
		if c.Level != LevelOK {
			t.Errorf("%s: %+v", name, c)
		}
	}
	if m["loom"].Detail != "v9.9.9" || !strings.Contains(m["project"].Detail, "1 prompts") || !strings.Contains(m["registry"].Detail, "https://r.example") {
		t.Errorf("%+v", m)
	}
	if Failed(cs) {
		t.Error("Failed")
	}
}

// Optional pieces are warnings that say which commands need them.
func TestSystemMissingOptionalPartsWarn(t *testing.T) {
	dir := sysProject(t, "", nil)
	cs := System(env(dir, nil))
	m := byName(cs)
	for _, name := range []string{"git", "model API key", "registry", "loomlocker"} {
		if m[name].Level != LevelWarn || m[name].Hint == "" {
			t.Errorf("%s should warn with a hint: %+v", name, m[name])
		}
	}
	if !strings.Contains(m["git"].Hint, "loom blame") || !strings.Contains(m["model API key"].Hint, "loom test") || !strings.Contains(m["registry"].Hint, "loom install") {
		t.Errorf("hints must name the commands that need the piece: %+v", m)
	}
	if Failed(cs) {
		t.Error("missing optional tools must not fail the check")
	}
}

func TestSystemAPIKeyFollowsTheConfiguredProvider(t *testing.T) {
	dir := sysProject(t, "[testing]\nprovider = \"anthropic\"\ndefault_model = \"claude-x\"\n", nil)
	m := byName(System(env(dir, map[string]string{"ANTHROPIC_API_KEY": "k"})))
	if c := m["model API key"]; c.Level != LevelOK || !strings.Contains(c.Detail, "ANTHROPIC_API_KEY") || !strings.Contains(c.Detail, "claude-x") {
		t.Errorf("%+v", c)
	}
	// the Gemini key is irrelevant when the project uses Anthropic
	m = byName(System(env(dir, map[string]string{"GEMINI_API_KEY": "k"})))
	if m["model API key"].Level != LevelWarn || !strings.Contains(m["model API key"].Detail, "ANTHROPIC_API_KEY") {
		t.Errorf("%+v", m["model API key"])
	}
	// a custom variable name wins
	dir = sysProject(t, "[testing]\napi_key_env = \"MY_KEY\"\n", nil)
	if c := byName(System(env(dir, map[string]string{"MY_KEY": "k"})))["model API key"]; c.Level != LevelOK {
		t.Errorf("%+v", c)
	}
}

func TestSystemBrokenProjectFails(t *testing.T) {
	dir := sysProject(t, "", map[string]string{"Bad.prompt.loom": "prompt Bad {\n  what is this\n}\n"})
	cs := System(env(dir, nil))
	if c := byName(cs)["project"]; c.Level != LevelFail || !strings.Contains(c.Detail, "unexpected token in body") {
		t.Errorf("%+v", c)
	}
	if !Failed(cs) {
		t.Error("a library that does not load is a failure")
	}
	// a malformed loom.toml too
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "loom.toml"), []byte("not toml ["), 0o644)
	if c := byName(System(env(d, nil)))["project"]; c.Level != LevelFail {
		t.Errorf("%+v", c)
	}
}

func TestSystemOutsideAProjectIsOnlyAWarning(t *testing.T) {
	cs := System(env(t.TempDir(), nil))
	if c := byName(cs)["project"]; c.Level != LevelWarn || !strings.Contains(c.Hint, "loom init") {
		t.Errorf("%+v", c)
	}
	if Failed(cs) {
		t.Error("not being in a project is not an installation failure")
	}
}

func TestSystemAPIKeyForOpenAI(t *testing.T) {
	dir := sysProject(t, "[testing]\nprovider = \"openai\"\n", nil)
	if c := byName(System(env(dir, map[string]string{"OPENAI_API_KEY": "k"})))["model API key"]; c.Level != LevelOK || !strings.Contains(c.Detail, "OPENAI_API_KEY") || !strings.Contains(c.Detail, "gpt") {
		t.Errorf("%+v", c)
	}
}
