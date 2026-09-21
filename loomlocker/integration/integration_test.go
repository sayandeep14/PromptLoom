// Package integration drives the REAL binaries end to end: loomlocker (server + CLI),
// `loom execute --unlock`, and the Python and Go client libraries.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const password = "integration-pw"
const original = "API_KEY=real-secret-value\nDEBUG=1\n"

var bins struct{ locker, loom string }

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd() // .../loomlocker/integration
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func build(t *testing.T) {
	t.Helper()
	if bins.locker != "" {
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell-based integration test; Windows is covered separately")
	}
	root := repoRoot(t)
	dir, err := os.MkdirTemp("", "loomlocker-bins-")
	if err != nil {
		t.Fatal(err)
	}
	for name, spec := range map[string]struct{ dir, pkg string }{
		"loomlocker": {filepath.Join(root, "loomlocker"), "./cmd/loomlocker"},
		"loom":       {root, "./cmd/loom"},
	} {
		out := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", out, spec.pkg)
		cmd.Dir = spec.dir
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, b)
		}
	}
	bins.locker, bins.loom = filepath.Join(dir, "loomlocker"), filepath.Join(dir, "loom")
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

type project struct {
	dir, port string
}

func newProject(t *testing.T, recoverable bool) *project {
	t.Helper()
	build(t)
	p := &project{dir: t.TempDir(), port: freePort(t)}
	cfg := map[string]any{
		"secret": []string{".env"},
		"loomlocker": map[string]any{
			"active": true, "lockhost": "http://localhost", "port": p.port,
			"recoverable": recoverable, "unlock_duration_seconds": 2,
		},
		"custom": map[string]string{
			// reads the file the way an application does at startup
			"runproject": "cat .env > startup-read.txt",
		},
	}
	b, _ := json.Marshal(cfg)
	must(t, os.WriteFile(filepath.Join(p.dir, ".loom.config"), b, 0o644))
	must(t, os.WriteFile(p.env(), []byte(original), 0o644))
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (p *project) env() string { return filepath.Join(p.dir, ".env") }
func (p *project) read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// run runs a binary in the project dir with stdin, returning combined output and exit code.
func (p *project) run(t *testing.T, bin, stdin string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s %v: %v", bin, args, err)
	}
	return out.String(), code
}

// start launches `loomlocker start` (password + confirmation on stdin) and waits until it answers.
func (p *project) start(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bins.locker, "start")
	cmd.Dir = p.dir
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	must(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://127.0.0.1:" + p.port + "/api/ping"); err == nil {
			resp.Body.Close()
			return cmd
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("loomlocker did not start:\n%s", out.String())
	return nil
}

func (p *project) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (p *project) isLocked(t *testing.T) bool {
	return !strings.Contains(p.read(t, ".env"), "real-secret-value")
}

// -----------------------------------------------------------------------------

func TestExecuteUnlockFlow(t *testing.T) {
	p := newProject(t, false)
	p.start(t)

	out, code := p.run(t, bins.locker, "", nil, "lock")
	if code != 0 {
		t.Fatalf("lock: %d\n%s", code, out)
	}
	if !p.isLocked(t) {
		t.Fatal("`loomlocker lock` should replace the real value")
	}

	// 1. without --unlock the project only ever sees tokens
	if _, code := p.run(t, bins.loom, "", nil, "execute", "runproject"); code != 0 {
		t.Fatal("execute without --unlock failed")
	}
	if got := p.read(t, "startup-read.txt"); strings.Contains(got, "real-secret-value") || !strings.Contains(got, "lk_") {
		t.Errorf("without --unlock the app must see tokens, got %q", got)
	}

	// 2. a wrong password does not unlock, and the command does not run
	os.Remove(filepath.Join(p.dir, "startup-read.txt"))
	out, code = p.run(t, bins.loom, "wrong-password\n", nil, "execute", "runproject", "--unlock")
	if code == 0 || !strings.Contains(strings.ToLower(out), "invalid password") {
		t.Errorf("wrong password: exit %d\n%s", code, out)
	}
	if !p.isLocked(t) {
		t.Error("still locked after a wrong password")
	}

	// 3. the right password: the command reads REAL values, then the secrets re-lock by themselves
	out, code = p.run(t, bins.loom, password+"\n", nil, "execute", "runproject", "--unlock")
	if code != 0 {
		t.Fatalf("execute --unlock: %d\n%s", code, out)
	}
	if got := p.read(t, "startup-read.txt"); got != original {
		t.Errorf("the app must read the real values during startup: %q", got)
	}
	p.waitFor(t, "automatic re-lock", func() bool { return p.isLocked(t) })

	// 4. stopping while locked needs the password and puts the real values back
	out, code = p.run(t, bins.locker, "wrong\n", nil, "stop")
	if code == 0 {
		t.Errorf("stop with a wrong password should fail:\n%s", out)
	}
	if !p.isLocked(t) {
		t.Fatal("a failed stop must leave the secrets locked")
	}
	out, code = p.run(t, bins.locker, password+"\n", nil, "stop")
	if code != 0 {
		t.Fatalf("stop: %d\n%s", code, out)
	}
	if got := p.read(t, ".env"); got != original {
		t.Errorf("stop must restore the file exactly: %q", got)
	}
}

func TestExecuteWithoutAServerJustRuns(t *testing.T) {
	p := newProject(t, false) // no server started
	out, code := p.run(t, bins.loom, "", nil, "execute", "runproject", "--unlock")
	if code != 0 || !strings.Contains(out, "not running") {
		t.Errorf("exit %d\n%s", code, out)
	}
	if got := p.read(t, "startup-read.txt"); got != original {
		t.Errorf("startup-read.txt = %q", got)
	}
}

func TestNoUnauthenticatedWayToRevealSecrets(t *testing.T) {
	p := newProject(t, false)
	p.start(t)
	p.run(t, bins.locker, "", nil, "lock")
	base := "http://127.0.0.1:" + p.port + "/api"

	// every endpoint that does not carry the password must leave the file locked
	for _, req := range []struct{ method, path, body string }{
		{"GET", "/ping", ""}, {"GET", "/status", ""}, {"POST", "/lock", ""},
		{"POST", "/autolock", ""}, {"POST", "/unlock", `{}`}, {"POST", "/unlock", `{"password":""}`},
		{"POST", "/stop", ""},
	} {
		r, _ := http.NewRequest(req.method, base+req.path, strings.NewReader(req.body))
		if req.body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "real-secret-value") {
			t.Errorf("%s %s leaked the secret: %s", req.method, req.path, body)
		}
		if !p.isLocked(t) {
			t.Fatalf("%s %s unlocked the secrets without the password", req.method, req.path)
		}
	}
	// the server is only reachable on loopback
	conns, _ := exec.Command("sh", "-c", "netstat -an 2>/dev/null | grep LISTEN | grep '[.:]"+p.port+" '").Output()
	for _, line := range strings.Split(strings.TrimSpace(string(conns)), "\n") {
		if line != "" && !strings.Contains(line, "127.0.0.1") && !strings.Contains(line, "::1") {
			t.Errorf("listening on a non-loopback address: %s", line)
		}
	}
}

func TestRecoverAfterACrash(t *testing.T) {
	p := newProject(t, true)
	cmd := p.start(t)
	p.run(t, bins.locker, "", nil, "lock")
	if !p.isLocked(t) {
		t.Fatal("expected locked")
	}
	journal := filepath.Join(p.dir, ".loom.secret.lock")
	if _, err := os.Stat(journal); err != nil {
		t.Fatalf("recoverable mode should have written the journal: %v", err)
	}

	// the process is killed while the secrets are locked (kill -9: no cleanup runs)
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()

	// a new session refuses to start until the values are recovered
	out, code := p.run(t, bins.locker, password+"\n"+password+"\n", nil, "start")
	if code == 0 || !strings.Contains(out, "loomlocker recover") {
		t.Errorf("start with a leftover journal: exit %d\n%s", code, out)
	}

	out, code = p.run(t, bins.locker, "not-the-password\n", nil, "recover")
	if code == 0 || !strings.Contains(out, "wrong password") {
		t.Errorf("recover with a wrong password: exit %d\n%s", code, out)
	}
	if !p.isLocked(t) {
		t.Error("a failed recover must not change anything")
	}

	out, code = p.run(t, bins.locker, password+"\n", nil, "recover")
	if code != 0 {
		t.Fatalf("recover: %d\n%s", code, out)
	}
	if got := p.read(t, ".env"); got != original {
		t.Errorf("recovered file: %q", got)
	}
	if _, err := os.Stat(journal); err == nil {
		t.Error("the journal must be removed after a successful recovery")
	}
}

func TestPythonClientLibrary(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	p := newProject(t, false)
	p.start(t)
	p.run(t, bins.locker, "", nil, "lock")

	script := `
import sys
sys.path.insert(0, sys.argv[1])
from bloompy import Safe
seen = {}
def load():
    seen["during"] = open(".env").read()
Safe().silent().unlock().execute(load).autolock()
print("DURING=" + repr(seen["during"]))
`
	cmd := exec.Command(py, "-c", script, filepath.Join(repoRoot(t), "libs", "bloompy"))
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "LOOM_SESSION_PASSWORD="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "real-secret-value") {
		t.Errorf("the app must see the real value inside execute():\n%s", out)
	}
	// autolock() re-locks immediately, without waiting for the timer
	if !p.isLocked(t) {
		t.Error("autolock() must re-lock the secrets straight away")
	}

	// without the password the library degrades gracefully: secrets stay locked, code still runs
	cmd = exec.Command(py, "-c", script, filepath.Join(repoRoot(t), "libs", "bloompy"))
	cmd.Dir = p.dir
	out, err = cmd.CombinedOutput()
	if err != nil || strings.Contains(string(out), "real-secret-value") {
		t.Errorf("without a password: err=%v\n%s", err, out)
	}
}

func TestGoClientLibrary(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	p := newProject(t, false)
	p.start(t)
	p.run(t, bins.locker, "", nil, "lock")

	app := filepath.Join(t.TempDir(), "app")
	must(t, os.MkdirAll(app, 0o755))
	gomod := fmt.Sprintf("module example.com/app\n\ngo 1.22\n\nrequire github.com/sayandeep14/PromptLoom/libs/gloom v0.0.0\n\nreplace github.com/sayandeep14/PromptLoom/libs/gloom => %s\n",
		filepath.Join(repoRoot(t), "libs", "gloom"))
	must(t, os.WriteFile(filepath.Join(app, "go.mod"), []byte(gomod), 0o644))
	must(t, os.WriteFile(filepath.Join(app, "main.go"), []byte(`package main

import (
	"fmt"
	"os"

	"github.com/sayandeep14/PromptLoom/libs/gloom"
)

func main() {
	var seen string
	gloom.NewSafe().Silent().Unlock().Execute(func() {
		b, _ := os.ReadFile(".env")
		seen = string(b)
	}).Autolock()
	fmt.Printf("DURING=%q\n", seen)
}
`), 0o644))
	bin := filepath.Join(app, "app")
	if out, err := (&exec.Cmd{Path: mustLook(t, "go"), Args: []string{"go", "build", "-o", bin, "."}, Dir: app,
		Env: append(os.Environ(), "GOFLAGS=-mod=mod")}).CombinedOutput(); err != nil {
		t.Fatalf("build app: %v\n%s", err, out)
	}

	cmd := exec.Command(bin)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "LOOM_SESSION_PASSWORD="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("app: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "real-secret-value") {
		t.Errorf("the app must see the real value inside Execute():\n%s", out)
	}
	if !p.isLocked(t) {
		t.Error("Autolock() must re-lock straight away")
	}
}

func mustLook(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " not installed")
	}
	return p
}

func TestJavaClientLibrary(t *testing.T) {
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("javac not installed")
	}
	java := mustLook(t, "java")
	p := newProject(t, false)
	p.start(t)
	p.run(t, bins.locker, "", nil, "lock")

	src := filepath.Join(repoRoot(t), "libs", "loomj", "src", "main", "java", "dev", "promptloom", "loomj")
	work := t.TempDir()
	mainJava := filepath.Join(work, "Main.java")
	must(t, os.WriteFile(mainJava, []byte(`
import dev.promptloom.loomj.Safe;
import java.nio.file.*;

public class Main {
    public static void main(String[] args) throws Exception {
        final String[] seen = new String[1];
        new Safe().silent().unlock().execute(() -> {
            try { seen[0] = Files.readString(Path.of(".env")); } catch (Exception e) { throw new RuntimeException(e); }
        }).autolock();
        System.out.println("DURING=" + seen[0].replace("\n", "|"));
    }
}
`), 0o644))
	classes := filepath.Join(work, "classes")
	compile := exec.Command(javac, "-d", classes, mainJava,
		filepath.Join(src, "LockerConfig.java"), filepath.Join(src, "LoomLockerClient.java"), filepath.Join(src, "Safe.java"))
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	cmd := exec.Command(java, "-cp", classes, "Main")
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(), "LOOM_SESSION_PASSWORD="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "real-secret-value") {
		t.Errorf("the app must see the real value inside execute():\n%s", out)
	}
	if !p.isLocked(t) {
		t.Error("autolock() must re-lock straight away")
	}
}
