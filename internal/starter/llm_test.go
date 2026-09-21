package starter

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
	"github.com/sayandeep14/PromptLoom/internal/validate"
	"github.com/sayandeep14/PromptLoom/internal/workspace"
)

// The DSL reference is what the model imitates when generating a library, so its
// example must be valid, warning-free v2, and the text must not teach legacy syntax.
func TestDSLReferenceTeachesOnlyV2(t *testing.T) {
	start := strings.Index(dslReference, "\nExample:\n")
	if start < 0 {
		t.Fatal("no Example section in dslReference")
	}
	var lines []string
	for _, l := range strings.Split(dslReference[start+len("\nExample:\n"):], "\n") {
		lines = append(lines, strings.TrimPrefix(l, "  "))
	}
	example := strings.Join(lines, "\n")

	nodes, err := parser.Parse("example.loom", example)
	if err != nil {
		t.Fatalf("the example in the LLM prompt does not parse: %v\n%s", err, example)
	}
	reg := registry.New()
	if err := reg.Register(nodes); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	for _, d := range validate.Validate(reg, cfg) {
		t.Errorf("the example in the LLM prompt has a diagnostic: %s", d)
	}
	rp, err := resolve.Resolve("CodeReviewer", reg)
	if err != nil {
		t.Fatal(err)
	}
	if len(rp.Instructions) != 3 || len(rp.Format) != 4 {
		t.Errorf("from(parent[0]) should extend the inherited lists: instructions=%v format=%v", rp.Instructions, rp.Format)
	}

	// The prose may mention the old operators only to forbid them.
	lower := strings.ToLower(dslReference)
	for _, phrase := range []string{"to append", "to remove", "use += to extend", "append to existing"} {
		if strings.Contains(lower, phrase) {
			t.Errorf("dslReference still teaches legacy syntax: %q", phrase)
		}
	}
	if !strings.Contains(dslReference, "Never use  +=") {
		t.Error("the reference should explicitly forbid += / -= / bare colon")
	}
}

// `loom start` used to send its API key in the URL query (and read the reply without a limit),
// like the other copies of the client that now live in one place.
func TestGenerateLLMUsesTheSharedClientSafely(t *testing.T) {
	const key = "AIza-STARTER-SECRET-KEY"
	var gotPath, gotQuery, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotHeader = r.URL.Path, r.URL.RawQuery, r.Header.Get("x-goog-api-key")
		plan := `[{"name":"Base.prompt.loom","type":"prompt","description":"d","content":"prompt Base {\n  persona :=\n    p\n}\n"}]`
		json.NewEncoder(w).Encode(map[string]any{"candidates": []any{map[string]any{
			"content": map[string]any{"parts": []any{map[string]any{"text": "```json\n" + plan + "\n```"}}}}}})
	}))
	defer srv.Close()
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	defer func() { llm.GeminiBaseURL = old }()
	t.Setenv("GEMINI_API_KEY", key)

	plan, err := GenerateLLM(&workspace.Info{}, config.Defaults(), TierMinimal)
	if err != nil || len(plan.Files) != 1 || plan.Files[0].Name != "Base.prompt.loom" {
		t.Fatalf("%+v %v", plan, err)
	}
	if gotQuery != "" || strings.Contains(gotPath, key) || gotHeader != key {
		t.Errorf("the key must travel in the header only: path=%q query=%q header=%q", gotPath, gotQuery, gotHeader)
	}

	// no key: a clear error, nothing sent
	t.Setenv("GEMINI_API_KEY", "")
	if _, err := GenerateLLM(&workspace.Info{}, config.Defaults(), TierMinimal); err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("%v", err)
	}
}
