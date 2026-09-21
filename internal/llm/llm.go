// Package llm is the one client every PromptLoom feature uses to talk to a model provider
// (Gemini, Anthropic, OpenAI): `loom test`, `loom summarize`, `loom start`, and whatever calls a
// model next. Keeping it in one place keeps the protections in one place:
//
//   - the API key travels in a header, never in the URL, so it cannot show up in an error or a log;
//   - anything returned in an error is scrubbed of the key;
//   - the model name is validated (it becomes part of a URL path);
//   - responses are size-limited, and an unreadable body still yields an error that names the
//     HTTP status.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
)

// Provider names.
const (
	Gemini    = "gemini"
	Anthropic = "anthropic"
	OpenAI    = "openai"
)

// Endpoints. Variables so tests can point them at a fake server.
var (
	GeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	AnthropicURL  = "https://api.anthropic.com/v1/messages"
	OpenAIURL     = "https://api.openai.com/v1/chat/completions"
)

// MaxResponseBytes bounds how much of a response is read.
const MaxResponseBytes = 8 << 20

var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)

// Client is a configured connection to one provider and model.
type Client struct {
	Provider string // gemini | anthropic | openai
	Model    string
	APIKey   string
	KeyEnv   string        // the environment variable the key came from
	Timeout  time.Duration // per request; 0 means 60s
}

// Request is one prompt for the model.
type Request struct {
	System    string
	User      string
	MaxTokens int // 0: the provider's default
}

// FromConfig builds a Client from the [testing] section of loom.toml: provider, model, and the
// API key read from the configured environment variable. Errors say what to change.
func FromConfig(cfg *config.Config) (*Client, error) {
	return New(cfg, "", "")
}

// New is FromConfig with an optional override of the provider and/or model, for features that
// compare several models. An override of the provider uses THAT provider's key variable and
// default model, not the project's.
func New(cfg *config.Config, provider, model string) (*Client, error) {
	overridden := provider != ""
	if provider == "" {
		provider = cfg.Testing.Provider
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = Gemini
	}
	defEnv, defModel, ok := config.ProviderDefaults(provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q (supported: gemini, anthropic, openai)", provider)
	}
	sameProvider := !overridden || strings.EqualFold(provider, cfg.Testing.Provider) || (cfg.Testing.Provider == "" && provider == Gemini)

	envVar := defEnv
	if sameProvider && cfg.Testing.APIKeyEnv != "" {
		envVar = cfg.Testing.APIKeyEnv
	}
	if model == "" {
		model = defModel
		if sameProvider && cfg.Testing.DefaultModel != "" {
			model = cfg.Testing.DefaultModel
		}
	}
	key := os.Getenv(envVar)
	if key == "" {
		return nil, fmt.Errorf("API key not set: $%s is empty\nAdd it to .loomsecret or export it in your shell", envVar)
	}
	timeout := time.Duration(cfg.Testing.TimeoutSec) * time.Second
	return &Client{Provider: provider, Model: model, APIKey: key, KeyEnv: envVar, Timeout: timeout}, nil
}

// ParseSpec splits "model" or "provider:model" (provider is one of gemini, anthropic, openai;
// any other colon belongs to the model name, as in "model:v1").
func ParseSpec(spec string) (provider, model string) {
	spec = strings.TrimSpace(spec)
	if head, rest, ok := strings.Cut(spec, ":"); ok {
		switch strings.ToLower(head) {
		case Gemini, Anthropic, OpenAI:
			return strings.ToLower(head), rest
		}
	}
	return "", spec
}

// Complete sends the request and returns the model's text.
func (c *Client) Complete(ctx context.Context, r Request) (string, error) {
	if !modelNameRe.MatchString(c.Model) {
		return "", fmt.Errorf("invalid model name %q", c.Model)
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch c.Provider {
	case Gemini:
		return c.gemini(ctx, r)
	case Anthropic:
		return c.anthropic(ctx, r)
	case OpenAI:
		return c.openai(ctx, r)
	}
	return "", fmt.Errorf("unknown provider %q (supported: gemini, anthropic, openai)", c.Provider)
}

// post sends a JSON body and returns the response body and status.
func (c *Client) post(ctx context.Context, url string, payload any, headers map[string]string) ([]byte, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// A transport error echoes the URL; the wrapped cause is what matters, scrubbed of the key.
		msg := err.Error()
		if u := errors.Unwrap(err); u != nil {
			msg = u.Error()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			msg = "timed out"
		}
		return nil, 0, fmt.Errorf("request to %s failed: %s", req.URL.Host, scrub(msg, c.APIKey))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading the response from %s: %s", req.URL.Host, scrub(err.Error(), c.APIKey))
	}
	return data, resp.StatusCode, nil
}

func scrub(s, key string) string {
	if key == "" {
		return s
	}
	return strings.ReplaceAll(s, key, "***")
}

func snippet(b []byte) string {
	t := strings.TrimSpace(string(b))
	if r := []rune(t); len(r) > 200 {
		t = string(r[:200]) + "…"
	}
	if t == "" {
		return "(empty body)"
	}
	return t
}

func unreadable(provider string, status int, body []byte) error {
	return fmt.Errorf("%s returned HTTP %d with an unreadable body: %s", provider, status, snippet(body))
}

// ---- Gemini ----

func (c *Client) gemini(ctx context.Context, r Request) (string, error) {
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	req := struct {
		SystemInstruction *content  `json:"systemInstruction,omitempty"`
		Contents          []content `json:"contents"`
		GenerationConfig  *struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		} `json:"generationConfig,omitempty"`
	}{Contents: []content{{Role: "user", Parts: []part{{Text: r.User}}}}}
	if r.System != "" {
		req.SystemInstruction = &content{Parts: []part{{Text: r.System}}}
	}
	if r.MaxTokens > 0 {
		req.GenerationConfig = &struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		}{r.MaxTokens}
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", GeminiBaseURL, c.Model)
	data, status, err := c.post(ctx, url, req, map[string]string{"x-goog-api-key": c.APIKey})
	if err != nil {
		return "", err
	}
	var resp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"` // a reasoning summary, not part of the answer
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", unreadable("Gemini", status, data)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("Gemini API error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", noContent("Gemini", status)
	}
	var sb strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		if !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), nil
}

// ---- Anthropic ----

func (c *Client) anthropic(ctx context.Context, r Request) (string, error) {
	max := r.MaxTokens
	if max <= 0 {
		max = 4096
	}
	req := struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		System    string `json:"system,omitempty"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{Model: c.Model, MaxTokens: max, System: r.System}
	req.Messages = append(req.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{"user", r.User})

	data, status, err := c.post(ctx, AnthropicURL, req, map[string]string{
		"x-api-key": c.APIKey, "anthropic-version": "2023-06-01",
	})
	if err != nil {
		return "", err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", unreadable("Anthropic", status, data)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("Anthropic API error (%s): %s", resp.Error.Type, resp.Error.Message)
	}
	// Only text blocks are the answer; thinking and tool_use blocks are not. (A block with no type,
	// as some Anthropic-compatible gateways send, counts as text.)
	var sb strings.Builder
	found := false
	for _, p := range resp.Content {
		if p.Type == "text" || p.Type == "" {
			sb.WriteString(p.Text)
			found = true
		}
	}
	if !found {
		return "", noContent("Anthropic", status)
	}
	return sb.String(), nil
}

// ---- OpenAI ----

func (c *Client) openai(ctx context.Context, r Request) (string, error) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	req := struct {
		Model     string `json:"model"`
		Messages  []msg  `json:"messages"`
		MaxTokens int    `json:"max_tokens,omitempty"`
	}{Model: c.Model, MaxTokens: r.MaxTokens}
	if r.System != "" {
		req.Messages = append(req.Messages, msg{"system", r.System})
	}
	req.Messages = append(req.Messages, msg{"user", r.User})

	data, status, err := c.post(ctx, OpenAIURL, req, map[string]string{"Authorization": "Bearer " + c.APIKey})
	if err != nil {
		return "", err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", unreadable("OpenAI", status, data)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("OpenAI API error (%s): %s", resp.Error.Type, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", noContent("OpenAI", status)
	}
	return resp.Choices[0].Message.Content, nil
}

func noContent(provider string, status int) error {
	if status >= 400 {
		return fmt.Errorf("%s returned HTTP %d with no content", provider, status)
	}
	return fmt.Errorf("%s returned no content", provider)
}
