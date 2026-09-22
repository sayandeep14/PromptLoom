package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/usage"
)

func resetUsageFlags() {
	usageSince, usageCommand, usageModel, usageJSON, usageClear = "", "", "", false, false
}

func usageProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(benchCliToml), 0o644)
	return dir
}

func TestUsageCommandWithNoLedgerYet(t *testing.T) {
	dir := usageProject(t)
	t.Chdir(dir)
	resetUsageFlags()
	out, err := captureStdout(t, func() error { return runUsage(usageCmd, nil) })
	if err != nil || !strings.Contains(out, "no usage recorded yet") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestUsageCommandSummarizesAndFilters(t *testing.T) {
	dir := usageProject(t)
	t.Chdir(dir)
	resetUsageFlags()
	log := usage.Open(usage.DefaultPath(dir))
	log.Append(usage.Record{Command: "run", Provider: "gemini", Model: "gemini-2.5-flash", Input: 10, Output: 2})
	log.Append(usage.Record{Command: "eval", Role: "judge", Provider: "anthropic", Model: "claude-x", Input: 100, Output: 20})

	out, err := captureStdout(t, func() error { return runUsage(usageCmd, nil) })
	if err != nil || !strings.Contains(out, "2 call(s)") {
		t.Fatalf("%v\n%s", err, out)
	}

	usageCommand = "run"
	out, err = captureStdout(t, func() error { return runUsage(usageCmd, nil) })
	if err != nil || !strings.Contains(out, "1 call(s)") || strings.Contains(out, "claude") {
		t.Fatalf("%v\n%s", err, out)
	}
	resetUsageFlags()

	usageModel = "anthropic:claude-x"
	out, err = captureStdout(t, func() error { return runUsage(usageCmd, nil) })
	if err != nil || !strings.Contains(out, "1 call(s)") {
		t.Fatalf("%v\n%s", err, out)
	}
	resetUsageFlags()
}

func TestUsageCommandBadSinceIsAClearError(t *testing.T) {
	dir := usageProject(t)
	t.Chdir(dir)
	resetUsageFlags()
	usageSince = "not-a-date"
	if err := runUsage(usageCmd, nil); err == nil || !strings.Contains(err.Error(), "--since") {
		t.Errorf("%v", err)
	}
	resetUsageFlags()
}

func TestUsageCommandJSON(t *testing.T) {
	dir := usageProject(t)
	t.Chdir(dir)
	resetUsageFlags()
	usage.Open(usage.DefaultPath(dir)).Append(usage.Record{Command: "run", Provider: "gemini", Model: "x", Input: 1, Output: 1})
	usageJSON = true
	out, err := captureStdout(t, func() error { return runUsage(usageCmd, nil) })
	if err != nil || !strings.Contains(out, `"Calls": 1`) {
		t.Fatalf("%v\n%s", err, out)
	}
	resetUsageFlags()
}

func TestUsageCommandClearDeletesTheLedger(t *testing.T) {
	dir := usageProject(t)
	t.Chdir(dir)
	resetUsageFlags()
	path := usage.DefaultPath(dir)
	usage.Open(path).Append(usage.Record{Command: "run"})
	if _, err := os.Stat(path); err != nil {
		t.Fatal("ledger should exist before --clear")
	}
	usageClear = true
	if err := runUsage(usageCmd, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("--clear must remove the ledger")
	}
	// --clear on an already-missing ledger is not an error
	if err := runUsage(usageCmd, nil); err != nil {
		t.Errorf("%v", err)
	}
	resetUsageFlags()
}
