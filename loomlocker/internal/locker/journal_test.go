package locker

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	icrypto "github.com/sayandeepgiri/promptloom/loomlocker/internal/crypto"
)

func TestJournalRoundTripAndProperties(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, JournalFilename)
	key, salt, err := icrypto.DeriveKey("pw", nil)
	if err != nil {
		t.Fatal(err)
	}
	mapping := map[string]map[string]string{"/p/.env": {`"lk_0123456789abcdef"`: `"hunter2"`}}
	if err := WriteJournal(path, key, salt, mapping); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(path)
	for _, leak := range []string{"hunter2", "/p/.env", "lk_0123456789abcdef", "pw"} {
		if bytes.Contains(raw, []byte(leak)) {
			t.Errorf("the recovery file leaks %q in clear text", leak)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("recovery file mode = %o, want 0600", fi.Mode().Perm())
		}
	}

	got, err := ReadJournal(path, "pw")
	if err != nil || got["/p/.env"][`"lk_0123456789abcdef"`] != `"hunter2"` {
		t.Errorf("read back: %v %v", got, err)
	}
	if _, err := ReadJournal(path, "wrong"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong password: %v", err)
	}
}

func TestJournalRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, JournalFilename)
	for name, content := range map[string][]byte{
		"empty": {}, "not a journal": []byte("hello world, this is long enough to pass the length check"),
		"truncated": []byte("LOOMJRNL1short"),
	} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadJournal(path, "pw"); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
	// a valid journal with one flipped byte must fail, not restore wrong values
	key, salt, _ := icrypto.DeriveKey("pw", nil)
	if err := WriteJournal(path, key, salt, map[string]map[string]string{"f": {"a": "b"}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	raw[len(raw)-3] ^= 0xff
	os.WriteFile(path, raw, 0o600)
	if _, err := ReadJournal(path, "pw"); err == nil {
		t.Error("a tampered journal must not decrypt")
	}
	if err := WriteJournal(path, key, []byte("short"), nil); err == nil {
		t.Error("a bad salt length must be an error")
	}
}

// The journal must be saved BEFORE any file changes; if it cannot be saved, nothing changes.
func TestBeforeWriteHookRunsFirstAndCanVeto(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, ".env", "K=secret\n", 0o644)
	st := NewState()

	var sawFileAtHookTime string
	var sawMapping map[string]map[string]string
	err := LockSecretsWith([]string{".env"}, dir, st, func(m map[string]map[string]string) error {
		sawFileAtHookTime = read(t, p)
		sawMapping = m
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawFileAtHookTime != "K=secret\n" {
		t.Errorf("the hook must run before the file is modified, saw %q", sawFileAtHookTime)
	}
	if len(sawMapping) != 1 {
		t.Errorf("the hook must receive the complete mapping: %v", sawMapping)
	}

	// veto: unlock, then make the hook fail
	if err := UnlockSecrets(nil, dir, st); err != nil {
		t.Fatal(err)
	}
	st = NewState()
	err = LockSecretsWith([]string{".env"}, dir, st, func(map[string]map[string]string) error {
		return errors.New("disk full")
	})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("hook failure must abort the lock: %v", err)
	}
	if got := read(t, p); got != "K=secret\n" || st.SecretCount() != 0 {
		t.Errorf("nothing may change when the journal cannot be saved: %q count=%d", got, st.SecretCount())
	}
}

func TestRestoreFromMappingAfterACrash(t *testing.T) {
	dir := t.TempDir()
	original := "export A=\"one two\"\nB=3\n"
	p := write(t, dir, ".env", original, 0o644)

	st := NewState()
	var saved map[string]map[string]string
	if err := LockSecretsWith([]string{".env"}, dir, st, func(m map[string]map[string]string) error {
		saved = m
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// "crash": the in-memory State is gone; only the journal contents survive
	if read(t, p) == original {
		t.Fatal("file should be locked")
	}
	n, err := RestoreFromMapping(saved)
	if err != nil || n != 2 {
		t.Fatalf("restored %d, err=%v", n, err)
	}
	if got := read(t, p); got != original {
		t.Errorf("recovered file differs: %q", got)
	}
}
