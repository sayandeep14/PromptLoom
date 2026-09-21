package handlers

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/server/internal/models"
)

func router(st Store) http.Handler {
	a := New(st)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/vaults", a.ListVaults)
	mux.HandleFunc("GET /api/v1/vaults/{slug}", a.GetVault)
	mux.HandleFunc("GET /api/v1/vaults/{slug}/bundle", a.GetBundle)
	mux.HandleFunc("POST /api/v1/vaults", a.UploadVault)
	mux.HandleFunc("DELETE /api/v1/vaults/{slug}", a.DeleteVault)
	return mux
}

func req(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func validPack(slug string) models.Bundle {
	return models.Bundle{
		Name: "Pack " + slug, Slug: slug, Version: "1.0.0", Tags: []string{"go"},
		Files: []models.BundleFile{{Path: "prompts/A.prompt.loom", FileType: "prompt", Content: "prompt A {}"}},
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("bad JSON %q: %v", rec.Body.String(), err)
	}
	return v
}

func TestUploadThenReadBack(t *testing.T) {
	st := newFake()
	h := router(st)

	rec := req(h, "POST", "/api/v1/vaults", mustJSON(t, validPack("go-backend")))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	if got := decode[map[string]string](t, rec); got["status"] != "ok" || got["slug"] != "go-backend" {
		t.Errorf("upload body: %v", got)
	}

	rec = req(h, "GET", "/api/v1/vaults/go-backend/bundle", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("bundle: %d", rec.Code)
	}
	b := decode[models.Bundle](t, rec)
	if b.Slug != "go-backend" || len(b.Files) != 1 || b.Files[0].Content != "prompt A {}" {
		t.Errorf("bundle round trip wrong: %+v", b)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type %q", ct)
	}

	rec = req(h, "GET", "/api/v1/vaults/go-backend", "")
	if rec.Code != http.StatusOK || decode[models.Vault](t, rec).FileCount != 1 {
		t.Errorf("vault meta: %d %s", rec.Code, rec.Body)
	}
}

func TestListShape(t *testing.T) {
	st := newFake()
	h := router(st)

	// Empty list must be [] not null so clients can range over it safely.
	rec := req(h, "GET", "/api/v1/vaults", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"vaults":[]`) {
		t.Fatalf("empty list: %d %s", rec.Code, rec.Body)
	}

	for _, s := range []string{"b-pack", "a-pack"} {
		req(h, "POST", "/api/v1/vaults", mustJSON(t, validPack(s)))
	}
	list := decode[struct{ Vaults []models.ListItem }](t, req(h, "GET", "/api/v1/vaults", ""))
	if len(list.Vaults) != 2 || list.Vaults[0].Slug != "a-pack" {
		t.Errorf("list: %+v", list.Vaults)
	}
}

func TestUploadReplacesExisting(t *testing.T) {
	st := newFake()
	h := router(st)
	req(h, "POST", "/api/v1/vaults", mustJSON(t, validPack("p")))
	v2 := validPack("p")
	v2.Version = "2.0.0"
	if rec := req(h, "POST", "/api/v1/vaults", mustJSON(t, v2)); rec.Code != http.StatusOK {
		t.Fatal(rec.Body)
	}
	if got := decode[models.Bundle](t, req(h, "GET", "/api/v1/vaults/p/bundle", "")); got.Version != "2.0.0" {
		t.Errorf("version %q, want 2.0.0", got.Version)
	}
}

func TestNotFound(t *testing.T) {
	h := router(newFake())
	for _, p := range []string{"/api/v1/vaults/nope", "/api/v1/vaults/nope/bundle"} {
		rec := req(h, "GET", p, "")
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "nope") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestDelete(t *testing.T) {
	st := newFake()
	h := router(st)
	req(h, "POST", "/api/v1/vaults", mustJSON(t, validPack("gone")))

	if rec := req(h, "DELETE", "/api/v1/vaults/gone", ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := req(h, "GET", "/api/v1/vaults/gone", ""); rec.Code != http.StatusNotFound {
		t.Errorf("after delete: %d", rec.Code)
	}
	if rec := req(h, "DELETE", "/api/v1/vaults/never-existed", ""); rec.Code != http.StatusOK {
		t.Errorf("deleting a missing pack should be idempotent, got %d", rec.Code)
	}
}

func TestBadSlugsNeverReachStore(t *testing.T) {
	st := newFake()
	st.err = errBoom // any store call would produce a 500, not a 400
	h := router(st)
	for _, p := range []string{"/api/v1/vaults/BAD", "/api/v1/vaults/a%20b", "/api/v1/vaults/x!/bundle"} {
		if rec := req(h, "GET", p, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: %d", p, rec.Code)
		}
	}
	if rec := req(h, "DELETE", "/api/v1/vaults/BAD", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE bad slug: %d", rec.Code)
	}
}

func TestInvalidUploadsNeverReachStore(t *testing.T) {
	st := newFake()
	h := router(st)

	evil := validPack("x")
	evil.Files[0].Path = "../../etc/passwd"
	noFiles := validPack("x")
	noFiles.Files = nil

	cases := map[string]string{
		"not json":      "{{{",
		"empty object":  "{}",
		"traversal":     mustJSON(t, evil),
		"no files":      mustJSON(t, noFiles),
		"empty body":    "",
		"array not obj": "[]",
	}
	for name, body := range cases {
		if rec := req(h, "POST", "/api/v1/vaults", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: got %d %s", name, rec.Code, rec.Body)
		}
	}
	if st.upsert != 0 {
		t.Errorf("store was called %d times for invalid uploads", st.upsert)
	}
}

func TestValidationDetailsReturned(t *testing.T) {
	h := router(newFake())
	rec := req(h, "POST", "/api/v1/vaults", `{"slug":"Bad Slug","version":"x","files":[]}`)
	got := decode[struct {
		Error   string
		Details []string
	}](t, rec)
	if rec.Code != http.StatusBadRequest || got.Error != "invalid pack" || len(got.Details) < 3 {
		t.Errorf("got %d %+v", rec.Code, got)
	}
}

func TestOversizeBodyIs413(t *testing.T) {
	h := router(newFake())
	r := httptest.NewRequest("POST", "/api/v1/vaults", strings.NewReader(`{"name":"`+strings.Repeat("a", 100)+`"}`))
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 10)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d, want 413", rec.Code)
	}
}

func TestStoreErrorsAreHiddenAndLogged(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(nil)

	st := newFake()
	st.err = errBoom
	h := router(st)

	for _, c := range []struct{ method, path, body string }{
		{"GET", "/api/v1/vaults", ""},
		{"GET", "/api/v1/vaults/ok", ""},
		{"GET", "/api/v1/vaults/ok/bundle", ""},
		{"POST", "/api/v1/vaults", mustJSON(t, validPack("ok"))},
		{"DELETE", "/api/v1/vaults/ok", ""},
	} {
		rec := req(h, c.method, c.path, c.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: %d", c.method, c.path, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "password") || strings.Contains(body, "10.1.2.3") {
			t.Errorf("%s %s leaked internals: %s", c.method, c.path, body)
		}
	}
	if !strings.Contains(logs.String(), "password authentication failed") {
		t.Error("the real error should be logged server-side")
	}
}
