package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Stream sends the request and calls onDelta with each piece of the answer as it arrives. It
// returns the whole text (also when the stream was cut short, together with the error, so a caller
// can keep what was received), and the usage when the provider reported it.
//
// Client.Timeout is an IDLE timeout here: the call fails when no data arrives for that long, so a
// long answer that keeps flowing is never cut off. Cancelling ctx (Ctrl-C) stops the request.
func (c *Client) Stream(ctx context.Context, r Request, onDelta func(string)) (string, Usage, error) {
	if !modelNameRe.MatchString(c.Model) {
		return "", Usage{}, fmt.Errorf("invalid model name %q", c.Model)
	}
	idle := c.Timeout
	if idle <= 0 {
		idle = 60 * time.Second
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	watchdog := time.AfterFunc(idle, func() { cancel(errIdle) })
	defer watchdog.Stop()
	touch := func() { watchdog.Reset(idle) }

	var (
		url     string
		payload any
		headers map[string]string
		parse   func(event, data string, st *streamState) error
	)
	switch c.Provider {
	case Gemini:
		url = fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", GeminiBaseURL, c.Model)
		payload, headers, parse = c.geminiBody(r), map[string]string{"x-goog-api-key": c.APIKey}, parseGemini
	case Anthropic:
		url = AnthropicURL
		payload, headers, parse = c.anthropicBody(r, true), map[string]string{"x-api-key": c.APIKey, "anthropic-version": "2023-06-01"}, parseAnthropic
	case OpenAI:
		url = OpenAIURL
		payload, headers, parse = c.openaiBody(r, true), map[string]string{"Authorization": "Bearer " + c.APIKey}, parseOpenAI
	default:
		return "", Usage{}, fmt.Errorf("unknown provider %q (supported: gemini, anthropic, openai)", c.Provider)
	}

	resp, err := c.open(ctx, url, payload, headers)
	if err != nil {
		return "", Usage{}, c.cause(ctx, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes))
		return "", Usage{}, c.httpError(resp.StatusCode, body)
	}

	st := &streamState{}
	total := 0
	rd := bufio.NewReaderSize(resp.Body, 64<<10)
	var event string
	for {
		line, err := readLine(rd)
		if line != "" || err == nil {
			touch()
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				break
			}
			if perr := parse(event, data, st); perr != nil {
				return st.text.String(), st.usage, c.scrubErr(perr)
			}
			if d := st.take(); d != "" {
				total += len(d)
				if total > MaxResponseBytes {
					return st.text.String(), st.usage, fmt.Errorf("the answer is longer than %d bytes; stopped", MaxResponseBytes)
				}
				if onDelta != nil {
					onDelta(d)
				}
			}
		case line == "":
			event = ""
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return st.text.String(), st.usage, c.cause(ctx, err)
		}
	}
	if st.text.Len() == 0 && !st.sawAny {
		return "", st.usage, fmt.Errorf("%s returned an empty stream", c.Provider)
	}
	if c.OnUsage != nil {
		c.OnUsage(st.usage)
	}
	return st.text.String(), st.usage, nil
}

var errIdle = errors.New("idle")

// cause turns a transport error into one the user can act on.
func (c *Client) cause(ctx context.Context, err error) error {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errIdle):
		return fmt.Errorf("timed out: no data from %s for %s", c.Provider, c.idle())
	case errors.Is(cause, context.Canceled), errors.Is(err, context.Canceled):
		return context.Canceled
	}
	return c.scrubErr(err)
}

func (c *Client) idle() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 60 * time.Second
}

func (c *Client) scrubErr(err error) error {
	msg := err.Error()
	if u := errors.Unwrap(err); u != nil && strings.Contains(msg, "http") {
		msg = u.Error()
	}
	return errors.New(scrub(msg, c.APIKey))
}

// open sends the POST and returns the response without reading it.
func (c *Client) open(ctx context.Context, url string, payload any, headers map[string]string) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", req.URL.Host, err)
	}
	return resp, nil
}

// httpError describes a failed (non-stream) response from any provider.
func (c *Client) httpError(status int, body []byte) error {
	var v struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) == nil && v.Error != nil && v.Error.Message != "" {
		return fmt.Errorf("%s API error (HTTP %d): %s", c.Provider, status, scrub(v.Error.Message, c.APIKey))
	}
	return fmt.Errorf("%s returned HTTP %d: %s", c.Provider, status, scrub(snippet(body), c.APIKey))
}

// readLine reads one line (any length up to the reader's buffer chain) without the newline.
func readLine(rd *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		part, isPrefix, err := rd.ReadLine()
		sb.Write(part)
		if err != nil {
			return sb.String(), err
		}
		if !isPrefix {
			return sb.String(), nil
		}
		if sb.Len() > MaxResponseBytes {
			return sb.String(), fmt.Errorf("a single event is larger than %d bytes", MaxResponseBytes)
		}
	}
}

// streamState accumulates the answer while events are parsed.
type streamState struct {
	text    strings.Builder
	pending string
	usage   Usage
	sawAny  bool
}

func (s *streamState) add(delta string) {
	s.sawAny = true
	s.text.WriteString(delta)
	s.pending += delta
}

func (s *streamState) take() string {
	d := s.pending
	s.pending = ""
	return d
}

// ---- provider event parsers ----

func parseGemini(_, data string, st *streamState) error {
	var v struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text    string `json:"text"`
					Thought bool   `json:"thought"`
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
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return fmt.Errorf("unreadable Gemini stream event: %s", snippet([]byte(data)))
	}
	if v.Error != nil {
		return fmt.Errorf("Gemini API error %d: %s", v.Error.Code, v.Error.Message)
	}
	st.sawAny = true
	for _, cand := range v.Candidates {
		for _, p := range cand.Content.Parts {
			if !p.Thought {
				st.add(p.Text)
			}
		}
	}
	if v.Usage.Prompt > 0 || v.Usage.Candidate > 0 {
		st.usage = Usage{InputTokens: v.Usage.Prompt, OutputTokens: v.Usage.Candidate}
	}
	return nil
}

func parseAnthropic(event, data string, st *streamState) error {
	var v struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
		Message struct {
			Usage struct {
				Input  int `json:"input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
		Usage struct {
			Output int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return fmt.Errorf("unreadable Anthropic stream event: %s", snippet([]byte(data)))
	}
	if v.Type == "" {
		v.Type = event
	}
	switch v.Type {
	case "error":
		if v.Error != nil {
			return fmt.Errorf("Anthropic API error (%s): %s", v.Error.Type, v.Error.Message)
		}
		return fmt.Errorf("Anthropic stream error")
	case "message_start":
		st.sawAny = true
		st.usage.InputTokens = v.Message.Usage.Input
	case "content_block_delta":
		if v.Delta.Type == "text_delta" || v.Delta.Type == "" {
			st.add(v.Delta.Text)
		}
	case "message_delta":
		if v.Usage.Output > 0 {
			st.usage.OutputTokens = v.Usage.Output
		}
	}
	return nil
}

func parseOpenAI(_, data string, st *streamState) error {
	var v struct {
		Choices []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return fmt.Errorf("unreadable OpenAI stream event: %s", snippet([]byte(data)))
	}
	if v.Error != nil {
		return fmt.Errorf("OpenAI API error (%s): %s", v.Error.Type, v.Error.Message)
	}
	st.sawAny = true
	for _, ch := range v.Choices {
		st.add(ch.Delta.Content)
	}
	if v.Usage != nil {
		st.usage = Usage{InputTokens: v.Usage.Prompt, OutputTokens: v.Usage.Completion}
	}
	return nil
}
