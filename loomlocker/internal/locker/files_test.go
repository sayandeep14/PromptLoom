package locker

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	icrypto "github.com/sayandeepgiri/promptloom/loomlocker/internal/crypto"
)

func write(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // umask-proof
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// roundTrip locks then unlocks and returns (locked, restored) contents.
func roundTrip(t *testing.T, secrets []string, dir, file string) (locked, restored string) {
	t.Helper()
	st := NewState()
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatalf("lock: %v", err)
	}
	locked = read(t, filepath.Join(dir, file))
	if err := UnlockSecrets(secrets, dir, st); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	return locked, read(t, filepath.Join(dir, file))
}

func TestParseSecretEntry(t *testing.T) {
	cases := []struct{ in, file, key string }{
		{".loom.secret", ".loom.secret", ""},
		{".env:{API_KEY}", ".env", "API_KEY"},
		{"application.yaml:{kafka.consumer-id}", "application.yaml", "kafka.consumer-id"},
		{"config/app.yml:{a.b.c}", "config/app.yml", "a.b.c"},
	}
	for _, c := range cases {
		if f, k := ParseSecretEntry(c.in); f != c.file || k != c.key {
			t.Errorf("%q -> (%q,%q), want (%q,%q)", c.in, f, k, c.file, c.key)
		}
	}
}

// The file must come back BYTE FOR BYTE: quotes, `export`, spacing, comments, CRLF,
// a missing final newline. (The old code turned `export A="x y"` into `A=x y`.)
func TestEnvRoundTripIsByteExact(t *testing.T) {
	original := "# database\r\n" +
		"export DB_PASS=\"my pass word\"\r\n" +
		"API_KEY = 'abc=def#ghi'\r\n" +
		"PLAIN=value # inline comment\r\n" +
		"EMPTY=\r\n" +
		"\r\n" +
		"OTHER=untouched" // no trailing newline
	dir := t.TempDir()
	write(t, dir, ".env", original, 0o644)

	locked, restored := roundTrip(t, []string{".env"}, dir, ".env")

	if restored != original {
		t.Errorf("round trip changed the file\n--- want\n%q\n--- got\n%q", original, restored)
	}
	for _, secret := range []string{"my pass word", "abc=def#ghi", "value # inline comment", "untouched"} {
		if strings.Contains(locked, secret) {
			t.Errorf("locked file still contains %q:\n%s", secret, locked)
		}
	}
	// structure survives locking: export, quote style, spacing and comments stay
	for _, want := range []string{"# database\r\n", "export DB_PASS=\"lk_", "API_KEY = 'lk_", "EMPTY=\r\n"} {
		if !strings.Contains(locked, want) {
			t.Errorf("locked file lost structure %q:\n%q", want, locked)
		}
	}
}

func TestOnlyTheNamedKeyChanges(t *testing.T) {
	original := "A=1\nAPI_KEY=secret\nB=2\n"
	dir := t.TempDir()
	write(t, dir, ".env", original, 0o644)
	locked, restored := roundTrip(t, []string{".env:{API_KEY}"}, dir, ".env")

	lines := strings.Split(locked, "\n")
	if lines[0] != "A=1" || lines[2] != "B=2" {
		t.Errorf("other keys were modified: %q", locked)
	}
	if !strings.HasPrefix(lines[1], "API_KEY=lk_") || !icrypto.IsToken(strings.TrimPrefix(lines[1], "API_KEY=")) {
		t.Errorf("API_KEY not replaced by a token: %q", lines[1])
	}
	if restored != original {
		t.Errorf("restore differs: %q", restored)
	}
}

func TestDuplicateKeysAreEachRestored(t *testing.T) {
	original := "TOKEN=first\nX=1\nTOKEN=second\n"
	dir := t.TempDir()
	write(t, dir, ".env", original, 0o644)
	locked, restored := roundTrip(t, []string{".env:{TOKEN}"}, dir, ".env")
	if strings.Contains(locked, "first") || strings.Contains(locked, "second") {
		t.Errorf("a duplicate was left unlocked: %q", locked)
	}
	if restored != original {
		t.Errorf("duplicate keys collided: want %q got %q", original, restored)
	}
}

func TestLockingTwiceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "K=secret\n", 0o644)
	st := NewState()
	secrets := []string{".env"}
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	once := read(t, filepath.Join(dir, ".env"))
	count := st.SecretCount()
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, ".env")) != once || st.SecretCount() != count {
		t.Error("locking an already-locked file must change nothing")
	}
	if err := UnlockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, ".env")); got != "K=secret\n" {
		t.Errorf("restore after double lock: %q", got)
	}
}

func TestTokensAreFreshEachLock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "K=secret\n", 0o644)
	a, _ := roundTrip(t, []string{".env"}, dir, ".env")
	b, _ := roundTrip(t, []string{".env"}, dir, ".env")
	if a == b {
		t.Error("each lock should use new random tokens")
	}
}

func TestFileModeIsPreserved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	dir := t.TempDir()
	envPath := write(t, dir, ".env", "K=secret\n", 0o640)
	yamlPath := write(t, dir, "app.yaml", "db:\n  pass: hunter2\n", 0o600)
	st := NewState()
	secrets := []string{".env", "app.yaml:{db.pass}"}
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{envPath: 0o640, yamlPath: 0o600} {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode changed to %o (was %o) while locked", filepath.Base(p), fi.Mode().Perm(), want)
		}
	}
	if err := UnlockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]os.FileMode{envPath: 0o640, yamlPath: 0o600} {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != want {
			t.Errorf("%s: mode changed to %o (was %o) after unlock", filepath.Base(p), fi.Mode().Perm(), want)
		}
	}
}

// A failure part-way through must leave EVERYTHING as it was. The old code had already
// replaced the first file's values, then reported "not locked", so unlock did nothing
// and the real values were lost.
func TestFailedLockChangesNothing(t *testing.T) {
	dir := t.TempDir()
	first := write(t, dir, ".env", "A=alpha\n", 0o644)
	write(t, dir, "second.env", "B=beta\n", 0o644)
	st := NewState()

	err := LockSecrets([]string{".env", "second.env:{DOES_NOT_EXIST}"}, dir, st)
	if err == nil {
		t.Fatal("expected an error for the missing key")
	}
	if got := read(t, first); got != "A=alpha\n" {
		t.Errorf("first file was modified even though the lock failed: %q", got)
	}
	if st.SecretCount() != 0 || st.Locked {
		t.Errorf("state must be untouched after a failed lock: count=%d locked=%v", st.SecretCount(), st.Locked)
	}
}

func TestMissingFileIsAClearError(t *testing.T) {
	err := LockSecrets([]string{"nope.env"}, t.TempDir(), NewState())
	if err == nil || !strings.Contains(err.Error(), "nope.env") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestWriteFailureRollsBackEarlierFiles(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := t.TempDir()
	first := write(t, dir, ".env", "A=alpha\n", 0o644)
	write(t, dir, "ro/second.env", "B=beta\n", 0o644)
	if err := os.Chmod(filepath.Join(dir, "ro"), 0o500); err != nil { // cannot create temp files there
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(dir, "ro"), 0o755)

	st := NewState()
	if err := LockSecrets([]string{".env", "ro/second.env"}, dir, st); err == nil {
		t.Fatal("expected a write error")
	}
	if got := read(t, first); got != "A=alpha\n" {
		t.Errorf("first file must be rolled back, got %q", got)
	}
	if st.SecretCount() != 0 {
		t.Error("state must be empty after a failed lock")
	}
}

func TestSeveralEntriesOnOneFile(t *testing.T) {
	original := "A=1\nB=2\nC=3\n"
	dir := t.TempDir()
	write(t, dir, ".env", original, 0o644)
	locked, restored := roundTrip(t, []string{".env:{A}", ".env:{C}"}, dir, ".env")
	if !strings.Contains(locked, "B=2") || strings.Contains(locked, "A=1") || strings.Contains(locked, "C=3") {
		t.Errorf("locked: %q", locked)
	}
	if restored != original {
		t.Errorf("restored: %q", restored)
	}
}

func TestUnlockRestoresFromTheMappingNotTheConfig(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "A=1\nB=2\n", 0o644)
	st := NewState()
	if err := LockSecrets([]string{".env"}, dir, st); err != nil {
		t.Fatal(err)
	}
	// the config changes (or is empty) while locked: restore must still cover every locked file
	if err := UnlockSecrets(nil, dir, st); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, ".env")); got != "A=1\nB=2\n" {
		t.Errorf("restore should not depend on the current config: %q", got)
	}
}

func TestUserEditsWhileLockedAreKept(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".env", "A=1\n", 0o644)
	st := NewState()
	if err := LockSecrets([]string{".env"}, dir, st); err != nil {
		t.Fatal(err)
	}
	locked := read(t, p)
	// someone appends a line and deletes nothing
	if err := os.WriteFile(p, []byte(locked+"NEW=added\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UnlockSecrets(nil, dir, st); err != nil {
		t.Fatal(err)
	}
	if got := read(t, p); got != "A=1\nNEW=added\n" {
		t.Errorf("got %q", got)
	}
}

func TestWholeFileLockLeavesNonAssignmentsAlone(t *testing.T) {
	original := "# comment\n\nnot an assignment\nK=v\n"
	dir := t.TempDir()
	write(t, dir, ".loom.secret", original, 0o600)
	locked, restored := roundTrip(t, []string{".loom.secret"}, dir, ".loom.secret")
	if !strings.HasPrefix(locked, "# comment\n\nnot an assignment\nK=lk_") {
		t.Errorf("locked: %q", locked)
	}
	if restored != original {
		t.Errorf("restored: %q", restored)
	}
}

// ---- YAML ----

const sampleYAML = `# service config
app:
  name: demo            # not secret
  kafka:
    consumer-id: "abc-123"   # rotate quarterly
    brokers:
      - one:9092
      - two:9092
db:
  password: 'it''s a "secret"'
  url: jdbc:postgresql://localhost/app
名前: 秘密
plain: hunter2
`

func TestYAMLRoundTripKeepsEveryByte(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "application.yaml", sampleYAML, 0o644)
	secrets := []string{
		"application.yaml:{app.kafka.consumer-id}", "application.yaml:{db.password}",
		"application.yaml:{名前}", "application.yaml:{plain}",
	}
	locked, restored := roundTrip(t, secrets, dir, "application.yaml")

	if restored != sampleYAML {
		t.Errorf("YAML changed (comments, quoting and indentation must survive)\n--- want\n%s\n--- got\n%s", sampleYAML, restored)
	}
	for _, secret := range []string{"abc-123", "it''s", "秘密", "hunter2"} {
		if strings.Contains(locked, secret) {
			t.Errorf("locked YAML still contains %q:\n%s", secret, locked)
		}
	}
	for _, keep := range []string{"# service config", "name: demo            # not secret", "- one:9092", "url: jdbc:postgresql://localhost/app"} {
		if !strings.Contains(locked, keep) {
			t.Errorf("locking touched unrelated content %q:\n%s", keep, locked)
		}
	}
}

func TestYAMLErrorsAreClear(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", "app:\n  list:\n    - x\n  text: |\n    multi\n    line\n  ok: v\n", 0o644)
	cases := map[string]string{
		"a.yaml:{app.missing}": "not found",
		"a.yaml:{app.list}":    "not a scalar",
		"a.yaml:{app.text}":    "multi-line",
		"a.yaml:{nope.ok}":     "not found",
	}
	for entry, want := range cases {
		err := LockSecrets([]string{entry}, dir, NewState())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), want) {
			t.Errorf("%s: want an error containing %q, got %v", entry, want, err)
		}
	}
	write(t, dir, "bad.yaml", "a: [unclosed\n", 0o644)
	if err := LockSecrets([]string{"bad.yaml:{a}"}, dir, NewState()); err == nil {
		t.Error("invalid YAML must be an error, not silently skipped")
	}
}

func TestYAMLLockedFileStillParses(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", "a:\n  b: \"x: y # z\"\n  c: 1\n", 0o644)
	st := NewState()
	if err := LockSecrets([]string{"a.yaml:{a.b}"}, dir, st); err != nil {
		t.Fatal(err)
	}
	// the app still gets valid YAML with the same shape while locked
	if got := read(t, filepath.Join(dir, "a.yaml")); !strings.Contains(got, "c: 1") || !strings.Contains(got, "b: ") {
		t.Errorf("locked YAML lost structure: %q", got)
	}
}
