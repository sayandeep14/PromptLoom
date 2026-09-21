package locker

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Deliberately awkward: odd spacing, tabs, unicode, escapes, nesting, arrays, no final newline.
const sampleJSON = "{\n" +
	"  \"name\":   \"demo\",\n" +
	"  \"port\": 5432,\n" +
	"\t\"db\" : { \"user\": \"admin\", \"password\": \"hun\\\"ter2\\u00e9\" },\n" +
	"  \"servers\": [\n" +
	"    { \"host\": \"a\", \"password\": \"pw-one\" },\n" +
	"    { \"host\": \"b\", \"password\": \"pw-two\" }\n" +
	"  ],\n" +
	"  \"名前\": \"秘密\",\n" +
	"  \"nested\":{\"a\":{\"b\":{\"key\":\"deep-secret\"}}},\n" +
	"  \"empty\": \"\",\n" +
	"  \"tricky\": \"a, b: {c} [d] \\\\ \",\n" +
	"  \"enabled\": true\n" +
	"}"

func TestJSONRoundTripKeepsEveryByte(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", sampleJSON, 0o644)
	secrets := []string{
		"config.json:{db.password}", "config.json:{servers.0.password}", "config.json:{servers.1.password}",
		"config.json:{名前}", "config.json:{nested.a.b.key}", "config.json:{tricky}",
	}
	locked, restored := roundTrip(t, secrets, dir, "config.json")

	if restored != sampleJSON {
		t.Errorf("JSON changed\n--- want\n%s\n--- got\n%s", sampleJSON, restored)
	}
	for _, secret := range []string{"hun", "pw-one", "pw-two", "秘密", "deep-secret", "a, b: {c}"} {
		if strings.Contains(locked, secret) {
			t.Errorf("the locked file still contains %q:\n%s", secret, locked)
		}
	}
	// everything that is not a named secret is untouched, byte for byte
	for _, keep := range []string{`"name":   "demo",`, `"port": 5432,`, "\t\"db\" : { \"user\": \"admin\", ", `"empty": "",`, `"enabled": true`, `{ "host": "a", "password": "lk_`} {
		if !strings.Contains(locked, keep) {
			t.Errorf("locking touched unrelated content %q:\n%s", keep, locked)
		}
	}
	if strings.HasSuffix(locked, "\n") {
		t.Error("a missing final newline must stay missing")
	}
}

// While locked, the application still gets valid JSON of the same shape.
func TestLockedJSONStillParsesWithTheSameShape(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.json", sampleJSON, 0o644)
	st := NewState()
	if err := LockSecrets([]string{"config.json:{db.password}", "config.json:{servers.0.password}"}, dir, st); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Port    int                             `json:"port"`
		DB      struct{ User, Password string } `json:"db"`
		Servers []struct{ Host, Password string }
	}
	if err := json.Unmarshal([]byte(read(t, filepath.Join(dir, "config.json"))), &v); err != nil {
		t.Fatalf("the locked file is not valid JSON: %v", err)
	}
	if v.Port != 5432 || v.DB.User != "admin" || !strings.HasPrefix(v.DB.Password, "lk_") || !strings.HasPrefix(v.Servers[0].Password, "lk_") || v.Servers[1].Password != "pw-two" {
		t.Errorf("%+v", v)
	}
}

func TestJSONLockingTwiceIsIdempotentAndTokensAreFresh(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.json", `{"k": "secret"}`, 0o644)
	st := NewState()
	secrets := []string{"c.json:{k}"}
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	once := read(t, filepath.Join(dir, "c.json"))
	if err := LockSecrets(secrets, dir, st); err != nil || read(t, filepath.Join(dir, "c.json")) != once {
		t.Error("locking an already-locked file must change nothing")
	}
	if err := UnlockSecrets(secrets, dir, st); err != nil || read(t, filepath.Join(dir, "c.json")) != `{"k": "secret"}` {
		t.Errorf("%q", read(t, filepath.Join(dir, "c.json")))
	}
	a, _ := roundTrip(t, secrets, dir, "c.json")
	b, _ := roundTrip(t, secrets, dir, "c.json")
	if a == b {
		t.Error("each lock uses new tokens")
	}
}

func TestJSONBOMAndCRLFSurvive(t *testing.T) {
	dir := t.TempDir()
	src := "\xef\xbb\xbf{\r\n  \"k\": \"secret\",\r\n  \"x\": 1\r\n}\r\n"
	write(t, dir, "c.json", src, 0o644)
	locked, restored := roundTrip(t, []string{"c.json:{k}"}, dir, "c.json")
	if restored != src {
		t.Errorf("%q", restored)
	}
	if !strings.HasPrefix(locked, "\xef\xbb\xbf{") || strings.Contains(locked, "secret") || !strings.Contains(locked, "\r\n  \"x\": 1\r\n}") {
		t.Errorf("%q", locked)
	}
}

func TestJSONErrorsAreClear(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.json", `{"app": {"list": ["x", {"y": "z"}], "n": 42, "flag": false, "nothing": null, "obj": {"k": "v"}, "ok": "v"}, "dup": "1", "dup": "2"}`, 0o644)
	cases := map[string]string{
		"a.json:{app.missing}":   "not found",
		"a.json:{nope.ok}":       "not found",
		"a.json:{app.list}":      "not a value you can lock",
		"a.json:{app.obj}":       "not a value you can lock",
		"a.json:{app.n}":         "a number",
		"a.json:{app.flag}":      "a boolean",
		"a.json:{app.nothing}":   "null",
		"a.json:{app.list.9}":    "position 9 not found",
		"a.json:{app.list.x}":    "not a position",
		"a.json:{app.ok.deeper}": "not an object or array",
		"a.json:{dup}":           "more than once",
		"a.json":                 "needs the path",
	}
	for entry, want := range cases {
		err := LockSecrets([]string{entry}, dir, NewState())
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Errorf("%s: want an error containing %q, got %v", entry, want, err)
		}
	}
	// a failed entry changes nothing
	if read(t, filepath.Join(dir, "a.json")) == "" || strings.Contains(read(t, filepath.Join(dir, "a.json")), "lk_") {
		t.Error("nothing may be written when an entry fails")
	}
	for name, body := range map[string]string{"bad1.json": `{"a": "b",}`, "bad2.json": "// comment\n{\"a\": \"b\"}", "bad3.json": `{"a": `} {
		write(t, dir, name, body, 0o644)
		if err := LockSecrets([]string{name + ":{a}"}, dir, NewState()); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("%s: invalid JSON must be an error, not silently skipped: %v", name, err)
		}
	}
}

func TestJSONMixedWithOtherFileTypesInOneLock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env", "API_KEY=abc\n", 0o644)
	write(t, dir, "c.json", `{"db": {"pass": "hunter2"}}`, 0o644)
	write(t, dir, "a.yaml", "x:\n  y: yamlsecret\n", 0o644)
	secrets := []string{".env:{API_KEY}", "c.json:{db.pass}", "a.yaml:{x.y}"}
	st := NewState()
	if err := LockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".env", "c.json", "a.yaml"} {
		if got := read(t, filepath.Join(dir, f)); strings.Contains(got, "abc") || strings.Contains(got, "hunter2") || strings.Contains(got, "yamlsecret") {
			t.Errorf("%s still holds a secret: %s", f, got)
		}
	}
	if err := UnlockSecrets(secrets, dir, st); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "c.json")) != `{"db": {"pass": "hunter2"}}` || read(t, filepath.Join(dir, ".env")) != "API_KEY=abc\n" {
		t.Error("restore")
	}
}
