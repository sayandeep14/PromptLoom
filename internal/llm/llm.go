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

// lockedTokenRe matches the placeholder LoomLocker writes in place of a locked secret.
var lockedTokenRe = regexp.MustCompile(`^lk_[0-9a-f]{16}$`)

var modelNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,99}$`)

// Client is a configured connection to one provider and model.
type Client struct {
	Provider string // gemini | anthropic | openai
	Model    string
	APIKey   string
	KeyEnv   string        // the environment variable the key came from
	Timeout  time.Duration // per request; 0 means 60s

	// OnUsage, when set, is called after every successful Complete or Stream with whatever the
	// provider reported (best effort: a zero Usage means it reported nothing). Never called from
	// more than one goroutine at a time for a given call, but a Client itself is not otherwise
	// synchronized — as before, one Client is for one call or one conversation, not shared across
	// concurrent callers. See internal/usage, which sets this to record token/cost history.
	OnUsage func(Usage)
}

// Message is one earlier turn of a conversation.
type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

// Request is one prompt for the model. History holds earlier turns (oldest first); User is the
// new message.
type Request struct {
	System    string
	History   []Message
	User      string
	MaxTokens int // 0: the provider's default
}

// Usage is what the provider reported for a call, when it reports anything.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// turns is History followed by the new user message.
func (r Request) turns() []Message {
	out := append([]Message(nil), r.History...)
	return append(out, Message{Role: "user", Content: r.User})
}

// FromConfig builds a Client from the [testing] section of loom.toml: provider, model, and the
// API key read from the configured environment variable. Errors say what to change.
func FromConfig(cfg *config.Config) (*Client, error) {
	return New(cfg, "", "")
}

// Resolve works out which provider, model and key variable an (optionally overridden) request
// would use, without needing the key itself.
func Resolve(cfg *config.Config, provider, model string) (prov, mdl, keyEnv string, err error) {
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
		return "", "", "", fmt.Errorf("unknown provider %q (supported: gemini, anthropic, openai)", provider)
	}
	sameProvider := !overridden || strings.EqualFold(provider, cfg.Testing.Provider) || (cfg.Testing.Provider == "" && provider == Gemini)

	keyEnv = defEnv
	if sameProvider && cfg.Testing.APIKeyEnv != "" {
		keyEnv = cfg.Testing.APIKeyEnv
	}
	if model == "" {
		model = defModel
		if sameProvider && cfg.Testing.DefaultModel != "" {
			model = cfg.Testing.DefaultModel
		}
	}
	return provider, model, keyEnv, nil
}

// New is FromConfig with an optional override of the provider and/or model, for features that
// compare several models. An override of the provider uses THAT provider's key variable and
// default model, not the project's.
func New(cfg *config.Config, provider, model string) (*Client, error) {
	provider, model, envVar, err := Resolve(cfg, provider, model)
	if err != nil {
		return nil, err
	}
	key := os.Getenv(envVar)
	if key == "" {
		return nil, fmt.Errorf("API key not set: $%s is empty\nAdd it to .loomsecret or export it in your shell", envVar)
	}
	if lockedTokenRe.MatchString(key) {
		return nil, fmt.Errorf("the API key in $%s is a LoomLocker token (%s…), not a real key: the secret file is locked.\n"+
			"Run the command under `loom execute <name> --unlock`, or unlock first with `loomlocker unlock`", envVar, key[:6])
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

	var (
		text  string
		usage Usage
		err   error
	)
	switch c.Provider {
	case Gemini:
		text, usage, err = c.gemini(ctx, r)
	case Anthropic:
		text, usage, err = c.anthropic(ctx, r)
	case OpenAI:
		text, usage, err = c.openai(ctx, r)
	default:
		return "", fmt.Errorf("unknown provider %q (supported: gemini, anthropic, openai)", c.Provider)
	}
	if err == nil && c.OnUsage != nil {
		c.OnUsage(usage)
	}
	return text, err
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

type gPart struct {
	Text string `json:"text"`
}
type gContent struct {
	Role  string  `json:"role,omitempty"`
	Parts []gPart `json:"parts"`
}
type gRequest struct {
	SystemInstruction *gContent  `json:"systemInstruction,omitempty"`
	Contents          []gContent `json:"contents"`
	GenerationConfig  *struct {
		MaxOutputTokens int `json:"maxOutputTokens"`
	} `json:"generationConfig,omitempty"`
}

func (c *Client) geminiBody(r Request) gRequest {
	var req gRequest
	for _, m := range r.turns() {
		role := "user"
		if m.Role == "assistant" {
			role = "model" // Gemini's name for the assistant
		}
		req.Contents = append(req.Contents, gContent{Role: role, Parts: []gPart{{Text: m.Content}}})
	}
	if r.System != "" {
		req.SystemInstruction = &gContent{Parts: []gPart{{Text: r.System}}}
	}
	if r.MaxTokens > 0 {
		req.GenerationConfig = &struct {
			MaxOutputTokens int `json:"maxOutputTokens"`
		}{r.MaxTokens}
	}
	return req
}

func (c *Client) gemini(ctx context.Context, r Request) (string, Usage, error) {
	url := fmt.Sprintf("%s/models/%s:generateContent", GeminiBaseURL, c.Model)
	data, status, err := c.post(ctx, url, c.geminiBody(r), map[string]string{"x-goog-api-key": c.APIKey})
	if err != nil {
		return "", Usage{}, err
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
		Usage struct {
			Prompt    int `json:"promptTokenCount"`
			Candidate int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Message string `json:"message"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", Usage{}, unreadable("Gemini", status, data)
	}
	if resp.Error != nil {
		return "", Usage{}, fmt.Errorf("Gemini API error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", Usage{}, noContent("Gemini", status)
	}
	var sb strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		if !p.Thought {
			sb.WriteString(p.Text)
		}
	}
	return sb.String(), Usage{InputTokens: resp.Usage.Prompt, OutputTokens: resp.Usage.Candidate}, nil
}

// ---- Anthropic ----

type aMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type aRequest struct {
	Model     string     `json:"model"`
	MaxTokens int        `json:"max_tokens"`
	System    string     `json:"system,omitempty"`
	Stream    bool       `json:"stream,omitempty"`
	Messages  []aMessage `json:"messages"`
}

func (c *Client) anthropicBody(r Request, stream bool) aRequest {
	max := r.MaxTokens
	if max <= 0 {
		max = 4096
	}
	req := aRequest{Model: c.Model, MaxTokens: max, System: r.System, Stream: stream}
	for _, m := range r.turns() {
		req.Messages = append(req.Messages, aMessage{m.Role, m.Content})
	}
	return req
}

func (c *Client) anthropic(ctx context.Context, r Request) (string, Usage, error) {
	data, status, err := c.post(ctx, AnthropicURL, c.anthropicBody(r, false), map[string]string{
		"x-api-key": c.APIKey, "anthropic-version": "2023-06-01",
	})
	if err != nil {
		return "", Usage{}, err
	}
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			Input  int `json:"input_tokens"`
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", Usage{}, unreadable("Anthropic", status, data)
	}
	if resp.Error != nil {
		return "", Usage{}, fmt.Errorf("Anthropic API error (%s): %s", resp.Error.Type, resp.Error.Message)
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
		return "", Usage{}, noContent("Anthropic", status)
	}
	return sb.String(), Usage{InputTokens: resp.Usage.Input, OutputTokens: resp.Usage.Output}, nil
}

// ---- OpenAI ----

type oMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type oRequest struct {
	Model         string     `json:"model"`
	Messages      []oMessage `json:"messages"`
	MaxTokens     int        `json:"max_tokens,omitempty"`
	Stream        bool       `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
}

func (c *Client) openaiBody(r Request, stream bool) oRequest {
	req := oRequest{Model: c.Model, MaxTokens: r.MaxTokens, Stream: stream}
	if stream {
		req.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{true}
	}
	if r.System != "" {
		req.Messages = append(req.Messages, oMessage{"system", r.System})
	}
	for _, m := range r.turns() {
		req.Messages = append(req.Messages, oMessage{m.Role, m.Content})
	}
	return req
}

func (c *Client) openai(ctx context.Context, r Request) (string, Usage, error) {
	data, status, err := c.post(ctx, OpenAIURL, c.openaiBody(r, false), map[string]string{"Authorization": "Bearer " + c.APIKey})
	if err != nil {
		return "", Usage{}, err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", Usage{}, unreadable("OpenAI", status, data)
	}
	if resp.Error != nil {
		return "", Usage{}, fmt.Errorf("OpenAI API error (%s): %s", resp.Error.Type, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", Usage{}, noContent("OpenAI", status)
	}
	return resp.Choices[0].Message.Content, Usage{InputTokens: resp.Usage.Prompt, OutputTokens: resp.Usage.Completion}, nil
}

func noContent(provider string, status int) error {
	if status >= 400 {
		return fmt.Errorf("%s returned HTTP %d with no content", provider, status)
	}
	return fmt.Errorf("%s returned no content", provider)
}
