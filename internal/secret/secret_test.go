package secret

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// clearEnv makes sure the keys are unset before the test and restored after it.
func clearEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		k := k
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		})
	}
}

func TestLoadSetsMissingVariables(t *testing.T) {
	clearEnv(t, "LT_A", "LT_B", "LT_C")
	Load(write(t, "# comment\n\nLT_A=alpha\n  LT_B = beta  \nLT_C=with=equals=inside\nnot an assignment\n=novalue\n"))
	for k, want := range map[string]string{"LT_A": "alpha", "LT_B": "beta", "LT_C": "with=equals=inside"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestShellEnvironmentWins(t *testing.T) {
	clearEnv(t, "LT_KEEP")
	os.Setenv("LT_KEEP", "from-the-shell")
	Load(write(t, "LT_KEEP=from-the-file\n"))
	if got := os.Getenv("LT_KEEP"); got != "from-the-shell" {
		t.Errorf("a variable already exported must not be overridden: %q", got)
	}
}

func TestAnEmptyVariableIsFilledFromTheFile(t *testing.T) {
	clearEnv(t, "LT_EMPTY")
	os.Setenv("LT_EMPTY", "")
	Load(write(t, "LT_EMPTY=filled\n"))
	if got := os.Getenv("LT_EMPTY"); got != "filled" {
		t.Errorf("got %q", got)
	}
}

// Dotenv habits: quoted values and `export`. The quote characters must not end up inside an API key.
func TestQuotesAndExportAreUnderstood(t *testing.T) {
	clearEnv(t, "LT_DQ", "LT_SQ", "LT_EXP", "LT_MIX", "LT_INNER", "export LT_EXP")
	Load(write(t, "LT_DQ=\"abc 123\"\nLT_SQ='def'\nexport LT_EXP=ghi\nLT_MIX=\"unterminated\nLT_INNER=a\"b\"c\n"))
	cases := map[string]string{
		"LT_DQ": "abc 123", "LT_SQ": "def", "LT_EXP": "ghi",
		"LT_MIX": "\"unterminated", "LT_INNER": "a\"b\"c",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if os.Getenv("export LT_EXP") != "" {
		t.Error(`"export KEY" must not become an environment variable name`)
	}
}

func TestCRLFFiles(t *testing.T) {
	clearEnv(t, "LT_CRLF")
	Load(write(t, "LT_CRLF=value\r\n"))
	if got := os.Getenv("LT_CRLF"); got != "value" {
		t.Errorf("got %q (a trailing \\r would corrupt the key)", got)
	}
}

func TestLoadWithoutAFileIsHarmless(t *testing.T) {
	Load(t.TempDir())
	Load(filepath.Join(t.TempDir(), "does-not-exist"))
}

func TestTemplateIsLoadableAndSetsNothingUseful(t *testing.T) {
	clearEnv(t, "GEMINI_API_KEY")
	Load(write(t, TemplateContent))
	if got := os.Getenv("GEMINI_API_KEY"); got != "" {
		t.Errorf("the template's empty key must stay empty, got %q", got)
	}
}
