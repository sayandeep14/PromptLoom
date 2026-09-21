// Package testrunner sends rendered prompts to a model and asserts contract rules.
package testrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/contract"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
)

// Endpoints are variables so tests can point them at a local server.
var (
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
	anthropicURL  = "https://api.anthropic.com/v1/messages"
)

// maxResponseBytes caps how much of a model reply is read.
const maxResponseBytes = 8 << 20

var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)

// Result is the outcome of a single test run.
type Result struct {
	PromptName string
	Passed     bool
	Skipped    bool
	SkipReason string
	Failures   []contract.Failure
	Response   string
	Duration   time.Duration
	Err        error
}

// Options controls a test run.
type Options struct {
	Model    string
	Record   bool
	Compare  bool
	TestsDir string
}

// RunAll runs tests for every prompt in the registry that has a contract.
func RunAll(reg *registry.Registry, cfg *config.Config, cwd string, opts Options) []Result {
	nodes := reg.Prompts()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	var results []Result
	for _, node := range nodes {
		results = append(results, Run(node.Name, reg, cfg, cwd, opts))
	}
	return results
}

// Run runs the test for a single named prompt.
func Run(name string, reg *registry.Registry, cfg *config.Config, cwd string, opts Options) Result {
	start := time.Now()
	res := Result{PromptName: name}

	node, ok := reg.LookupPrompt(name)
	if !ok {
		res.Err = fmt.Errorf("prompt %q not found", name)
		return res
	}

	if node.Contract == nil {
		res.Skipped = true
		res.SkipReason = "no contract declared"
		return res
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		res.Err = fmt.Errorf("resolve: %w", err)
		return res
	}

	renderedPrompt := render.Render(rp, cfg)

	testsDir := opts.TestsDir
	if testsDir == "" {
		testsDir = filepath.Join(cwd, "tests")
	}
	input := loadFixture(testsDir, name)

	model := opts.Model
	if model == "" {
		model = cfg.Testing.DefaultModel
	}
	if model == "" {
		model = "gemini-2.0-flash"
	}

	if !modelNameRe.MatchString(model) {
		res.Err = fmt.Errorf("invalid model name %q", model)
		return res
	}

	apiKey, err := resolveAPIKey(cfg)
	if err != nil {
		res.Err = err
		return res
	}

	timeout := time.Duration(cfg.Testing.TimeoutSec) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	response, err := callModel(cfg.Testing.Provider, model, apiKey, renderedPrompt, input, timeout)
	if err != nil {
		res.Err = fmt.Errorf("model call failed: %w", err)
		return res
	}
	res.Response = response

	if opts.Record {
		if err := writeBaseline(testsDir, name, response); err != nil {
			res.Err = fmt.Errorf("record baseline: %w", err)
			return res
		}
	}

	if opts.Compare {
		baseline, err := readBaseline(testsDir, name)
		if err != nil {
			res.Err = fmt.Errorf("read baseline: %w", err)
			return res
		}
		failures := compareResponses(node.Contract, baseline, response)
		res.Failures = failures
		res.Passed = len(failures) == 0
		res.Duration = time.Since(start)
		return res
	}

	failures := contract.Check(node.Contract, response)
	res.Failures = failures
	res.Passed = len(failures) == 0
	res.Duration = time.Since(start)
	return res
}

func resolveAPIKey(cfg *config.Config) (string, error) {
	envVar := cfg.Testing.APIKeyEnv
	if envVar == "" {
		switch cfg.Testing.Provider {
		case "anthropic":
			envVar = "ANTHROPIC_API_KEY"
		default:
			envVar = "GEMINI_API_KEY"
		}
	}
	key := os.Getenv(envVar)
	if key == "" {
		return "", fmt.Errorf("API key not set: $%s is empty", envVar)
	}
	return key, nil
}

func loadFixture(testsDir, name string) string {
	path := filepath.Join(testsDir, name+".input.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultStub
	}
	return string(data)
}

func writeBaseline(testsDir, name, response string) error {
	path := filepath.Join(testsDir, name+".baseline.md")
	// a namespaced prompt name ("team/Reviewer") maps to a sub-directory
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(response), 0644)
}

func readBaseline(testsDir, name string) (string, error) {
	path := filepath.Join(testsDir, name+".baseline.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("baseline not found at %s — run with --record first", path)
	}
	return string(data), nil
}

// compareResponses checks if the new response passes the same contract as the baseline.
// Returns failures relative to the declared contract; also notes if response diverged.
func compareResponses(c *ast.ContractBlock, baseline, current string) []contract.Failure {
	baselineFailures := contract.Check(c, baseline)
	currentFailures := contract.Check(c, current)

	var extra []contract.Failure
	for _, f := range currentFailures {
		found := false
		for _, bf := range baselineFailures {
			if bf.Kind == f.Kind && bf.Detail == f.Detail {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, f)
		}
	}
	return extra
}

func callModel(provider, model, apiKey, systemPrompt, userInput string, timeout time.Duration) (string, error) {
	switch strings.ToLower(provider) {
	case "anthropic":
		return callAnthropic(model, apiKey, systemPrompt, userInput, timeout)
	case "", "gemini":
		return callGemini(model, apiKey, systemPrompt, userInput, timeout)
	default:
		return "", fmt.Errorf("unknown provider %q in [testing] (supported: gemini, anthropic)", provider)
	}
}

// --- Gemini REST client ---

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func callGemini(model, apiKey, systemPrompt, userInput string, timeout time.Duration) (string, error) {
	// The key travels in a header, never in the URL: Go's HTTP errors include the full URL,
	// so a key in the query string would be printed on any network failure.
	url := fmt.Sprintf("%s/%s:generateContent", geminiBaseURL, model)

	req := geminiRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: userInput}},
			},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", apiKey)

	respBody, status, err := do(httpReq, apiKey)
	if err != nil {
		return "", err
	}

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return "", fmt.Errorf("Gemini returned HTTP %d with a response that is not JSON: %s", status, snippet(respBody))
	}

	if gemResp.Error != nil {
		return "", fmt.Errorf("Gemini API error %d: %s", gemResp.Error.Code, gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 || len(gemResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned no content (finish_reason may indicate safety block)")
	}

	return gemResp.Candidates[0].Content.Parts[0].Text, nil
}

// --- Anthropic REST client ---

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func callAnthropic(model, apiKey, systemPrompt, userInput string, timeout time.Duration) (string, error) {
	url := anthropicURL

	req := anthropicRequest{
		Model:     model,
		MaxTokens: 2048,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userInput},
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	respBody, status, err := do(httpReq, apiKey)
	if err != nil {
		return "", err
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		return "", fmt.Errorf("Anthropic returned HTTP %d with a response that is not JSON: %s", status, snippet(respBody))
	}

	if antResp.Error != nil {
		return "", fmt.Errorf("Anthropic API error (%s): %s", antResp.Error.Type, antResp.Error.Message)
	}

	if len(antResp.Content) == 0 {
		return "", fmt.Errorf("Anthropic returned no content")
	}

	for _, block := range antResp.Content {
		if block.Type == "text" {
			return block.Text, nil
		}
	}
	return "", fmt.Errorf("Anthropic response contained no text block")
}

// do sends the request and returns the (size-limited) body and status. Any error is scrubbed
// of the API key and the URL before it can reach a terminal or a CI log.
func do(req *http.Request, apiKey string) ([]byte, int, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request to %s failed: %s", req.URL.Host, redact(errorText(err), apiKey))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading the response from %s: %s", req.URL.Host, redact(err.Error(), apiKey))
	}
	return body, resp.StatusCode, nil
}

// errorText unwraps *url.Error so the message does not repeat the request URL.
func errorText(err error) string {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok && u.Unwrap() != nil {
		return u.Unwrap().Error()
	}
	return err.Error()
}

func redact(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "***")
	}
	return s
}

// snippet returns the start of a body for an error message.
func snippet(b []byte) string {
	t := strings.TrimSpace(string(b))
	if len(t) > 200 {
		t = t[:200] + "…"
	}
	if t == "" {
		return "(empty body)"
	}
	return t
}

const defaultStub = `Please review the following code snippet for issues:

` + "```" + `java
public class UserService {
    public User findUser(String id) {
        String query = "SELECT * FROM users WHERE id = " + id;
        return db.execute(query);
    }
}
` + "```" + `

Identify any problems and suggest fixes.`
