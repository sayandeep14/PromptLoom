package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
)

// sseServer replays raw SSE text in the given chunks (so events can be split anywhere), with an
// optional pause between chunks.
type sseServer struct {
	chunks []string
	status int
	pause  time.Duration
	body   string // what the client sent
	req    *http.Request
}

func newSSE(t *testing.T, s *sseServer) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.body, s.req = string(b), r
		if s.status != 0 && s.status != 200 {
			w.WriteHeader(s.status)
			io.WriteString(w, strings.Join(s.chunks, ""))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for _, c := range s.chunks {
			io.WriteString(w, c)
			fl.Flush()
			if s.pause > 0 {
				select {
				case <-time.After(s.pause):
				case <-r.Context().Done():
					return
				}
			}
		}
	}))
	g, a, o := GeminiBaseURL, AnthropicURL, OpenAIURL
	GeminiBaseURL, AnthropicURL, OpenAIURL = srv.URL+"/v1beta", srv.URL+"/v1/messages", srv.URL+"/v1/chat/completions"
	t.Cleanup(func() { srv.Close(); GeminiBaseURL, AnthropicURL, OpenAIURL = g, a, o })
}

func collect(c *Client, r Request) (deltas []string, text string, u Usage, err error) {
	text, u, err = c.Stream(context.Background(), r, func(d string) { deltas = append(deltas, d) })
	return
}

const (
	geminiSSE = "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hel\"}]}}]}\r\n\r\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pondering\",\"thought\":true},{\"text\":\"lo \"}]}}]}\r\n\r\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"world\"}]}}],\"usageMetadata\":{\"promptTokenCount\":12,\"candidatesTokenCount\":3}}\r\n\r\n"

	anthropicSSE = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":25,\"output_tokens\":1}}}\n\n" +
		"event: ping\ndata: {\"type\":\"ping\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hmm\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello \"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"world\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":7}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	openaiSSE = "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":2}}\n\n" +
		"data: [DONE]\n\n"
)

func TestEachProviderStreamsItsFormat(t *testing.T) {
	cases := []struct {
		provider, sse string
		in, out       int
	}{
		{Gemini, geminiSSE, 12, 3}, {Anthropic, anthropicSSE, 25, 7}, {OpenAI, openaiSSE, 9, 2},
	}
	for _, c := range cases {
		s := &sseServer{chunks: []string{c.sse}}
		newSSE(t, s)
		deltas, text, u, err := collect(client(c.provider), Request{System: "sys", User: "hi"})
		if err != nil || text != "Hello world" && text != "Hel"+"lo world" {
			t.Errorf("%s: %q %v", c.provider, text, err)
		}
		if strings.Join(deltas, "") != text || len(deltas) < 2 {
			t.Errorf("%s: deltas %q must add up to the text and arrive separately", c.provider, deltas)
		}
		if u.InputTokens != c.in || u.OutputTokens != c.out {
			t.Errorf("%s: usage %+v", c.provider, u)
		}
		// the key: in a header, never the URL; the request asks for a stream
		if strings.Contains(s.req.URL.String(), secretKey) {
			t.Errorf("%s: key in the URL", c.provider)
		}
		if c.provider != Gemini && !strings.Contains(s.body, `"stream":true`) {
			t.Errorf("%s: %s", c.provider, s.body)
		}
		if c.provider == Gemini && s.req.URL.Query().Get("alt") != "sse" {
			t.Errorf("gemini must ask for SSE: %s", s.req.URL)
		}
		if c.provider == OpenAI && !strings.Contains(s.body, `"include_usage":true`) {
			t.Errorf("openai needs include_usage to report tokens: %s", s.body)
		}
	}
}

// Network chunks can end anywhere, including inside an event and inside a UTF-8 character.
func TestEventsSplitAtAnyByteAreReassembled(t *testing.T) {
	for _, tc := range []struct{ provider, sse string }{{Gemini, geminiSSE}, {Anthropic, anthropicSSE}, {OpenAI, openaiSSE}} {
		want := ""
		{
			newSSE(t, &sseServer{chunks: []string{tc.sse}})
			_, want, _, _ = collect(client(tc.provider), Request{User: "q"})
		}
		for _, size := range []int{1, 2, 3, 7, 19} {
			var chunks []string
			for i := 0; i < len(tc.sse); i += size {
				chunks = append(chunks, tc.sse[i:min(i+size, len(tc.sse))])
			}
			newSSE(t, &sseServer{chunks: chunks})
			_, got, _, err := collect(client(tc.provider), Request{User: "q"})
			if err != nil || got != want {
				t.Errorf("%s split every %d bytes: %q (want %q) %v", tc.provider, size, got, want, err)
			}
		}
	}
	// multi-byte characters survive being cut mid-character
	ev := "data: {\"choices\":[{\"delta\":{\"content\":\"日本語 é 😀\"}}]}\n\ndata: [DONE]\n\n"
	var chunks []string
	for i := 0; i < len(ev); i++ {
		chunks = append(chunks, ev[i:i+1])
	}
	newSSE(t, &sseServer{chunks: chunks})
	if _, got, _, err := collect(client(OpenAI), Request{User: "q"}); err != nil || got != "日本語 é 😀" {
		t.Errorf("%q %v", got, err)
	}
}

func TestHistoryIsSentInOrderWithProviderRoles(t *testing.T) {
	req := Request{System: "sys", User: "third", History: []Message{{"user", "first"}, {"assistant", "second"}}}
	for _, p := range []string{Gemini, Anthropic, OpenAI} {
		s := &sseServer{chunks: []string{map[string]string{Gemini: geminiSSE, Anthropic: anthropicSSE, OpenAI: openaiSSE}[p]}}
		newSSE(t, s)
		if _, _, _, err := collect(client(p), req); err != nil {
			t.Fatal(err)
		}
		i1, i2, i3 := strings.Index(s.body, "first"), strings.Index(s.body, "second"), strings.Index(s.body, "third")
		if !(0 < i1 && i1 < i2 && i2 < i3) {
			t.Errorf("%s: turns out of order: %s", p, s.body)
		}
		wantRole := map[string]string{Gemini: `"role":"model"`, Anthropic: `"role":"assistant"`, OpenAI: `"role":"assistant"`}[p]
		if !strings.Contains(s.body, wantRole) {
			t.Errorf("%s: the assistant turn needs %s: %s", p, wantRole, s.body)
		}
	}
	// the non-streaming call sends the same history
	f := newFake(t, 200, openaiOK)
	client(OpenAI).Complete(context.Background(), req)
	if !strings.Contains(f.bodies[0], "second") {
		t.Errorf("%s", f.bodies[0])
	}
}

func TestStreamErrors(t *testing.T) {
	cases := []struct {
		name, provider, want string
		s                    sseServer
	}{
		{"http 401 json", OpenAI, "bad key", sseServer{status: 401, chunks: []string{`{"error":{"message":"bad key","type":"invalid_request_error"}}`}}},
		{"http 500 html", Gemini, "HTTP 500", sseServer{status: 500, chunks: []string{"<html>oops</html>"}}},
		{"gemini error event", Gemini, "quota", sseServer{chunks: []string{"data: {\"error\":{\"code\":429,\"message\":\"quota exhausted\"}}\n\n"}}},
		{"anthropic error event", Anthropic, "overloaded_error", sseServer{chunks: []string{"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"}}},
		{"openai error chunk", OpenAI, "rate_limit", sseServer{chunks: []string{"data: {\"error\":{\"type\":\"rate_limit\",\"message\":\"slow down\"}}\n\n"}}},
		{"garbage event", Gemini, "unreadable Gemini stream event", sseServer{chunks: []string{"data: not json\n\n"}}},
		{"empty stream", OpenAI, "empty stream", sseServer{chunks: []string{": keep-alive\n\n"}}},
	}
	for _, c := range cases {
		s := c.s
		newSSE(t, &s)
		_, _, _, err := collect(client(c.provider), Request{User: "q"})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (want %q)", c.name, err, c.want)
		}
	}
	// what arrived before the failure is kept, so a caller can show it
	newSSE(t, &sseServer{chunks: []string{"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"x\"}}\n\n"}})
	_, text, _, err := collect(client(Anthropic), Request{User: "q"})
	if err == nil || text != "partial" {
		t.Errorf("partial text must be returned with the error: %q %v", text, err)
	}
}

func TestStreamNeverLeaksTheKey(t *testing.T) {
	g := GeminiBaseURL
	GeminiBaseURL = "http://127.0.0.1:1/v1beta"
	defer func() { GeminiBaseURL = g }()
	_, _, err := client(Gemini).Stream(context.Background(), Request{User: "q"}, nil)
	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Errorf("%v", err)
	}
	newSSE(t, &sseServer{status: 400, chunks: []string{`{"error":{"message":"invalid key ` + secretKey + `"}}`}})
	_, _, err = client(OpenAI).Stream(context.Background(), Request{User: "q"}, nil)
	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Errorf("a key echoed by the server must be scrubbed: %v", err)
	}
}

func TestCancellationStopsTheStreamAndKeepsWhatArrived(t *testing.T) {
	newSSE(t, &sseServer{
		chunks: []string{"data: {\"choices\":[{\"delta\":{\"content\":\"one \"}}]}\n\n", "data: {\"choices\":[{\"delta\":{\"content\":\"two \"}}]}\n\n", "data: {\"choices\":[{\"delta\":{\"content\":\"three\"}}]}\n\ndata: [DONE]\n\n"},
		pause:  300 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	var got []string
	start := time.Now()
	text, _, err := client(OpenAI).Stream(ctx, Request{User: "q"}, func(d string) {
		got = append(got, d)
		if len(got) == 1 {
			cancel() // Ctrl-C after the first piece
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
	if text != "one " || time.Since(start) > time.Second {
		t.Errorf("text %q after %s: it must stop promptly and keep what arrived", text, time.Since(start))
	}
}

// Timeout is an idle timeout for streams: a long answer that keeps flowing is fine, a stalled
// one is cut.
func TestIdleTimeout(t *testing.T) {
	ev := func(s string) string { return "data: {\"choices\":[{\"delta\":{\"content\":\"" + s + "\"}}]}\n\n" }
	// steady flow for longer than the timeout: succeeds
	newSSE(t, &sseServer{chunks: []string{ev("a"), ev("b"), ev("c"), ev("d"), ev("e"), "data: [DONE]\n\n"}, pause: 60 * time.Millisecond})
	c := client(OpenAI)
	c.Timeout = 150 * time.Millisecond
	if _, text, _, err := collect(c, Request{User: "q"}); err != nil || text != "abcde" {
		t.Errorf("a stream that keeps flowing must not time out: %q %v", text, err)
	}
	// silence: fails, saying so
	newSSE(t, &sseServer{chunks: []string{ev("a"), ev("b")}, pause: 2 * time.Second})
	c.Timeout = 100 * time.Millisecond
	_, text, _, err := collect(c, Request{User: "q"})
	if err == nil || !strings.Contains(err.Error(), "timed out: no data from openai") || text != "a" {
		t.Errorf("%q %v", text, err)
	}
}

func TestStreamBadModelAndProvider(t *testing.T) {
	c := client(Gemini)
	c.Model = "x/../y"
	if _, _, err := c.Stream(context.Background(), Request{User: "q"}, nil); err == nil || !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("%v", err)
	}
	if _, _, err := client("nope").Stream(context.Background(), Request{User: "q"}, nil); err == nil || !strings.Contains(err.Error(), "unknown provider") {
		t.Errorf("%v", err)
	}
}

// A locked secret file holds a token where the key was. Say so, instead of a confusing 401.
func TestLoomLockerTokenInPlaceOfAKeyIsRecognised(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "lk_7f3a9b2c1d4e5a6b")
	cfg := config.Defaults()
	_, err := New(cfg, "", "")
	if err == nil || !strings.Contains(err.Error(), "LoomLocker token") || !strings.Contains(err.Error(), "loom execute") || strings.Contains(err.Error(), "lk_7f3a9b2c1d4e5a6b") {
		t.Errorf("%v", err)
	}
	// a real key that merely starts with lk_ is not mistaken for a token
	t.Setenv("GEMINI_API_KEY", "lk_this-is-a-real-key-with-other-characters")
	if _, err := New(cfg, "", ""); err != nil {
		t.Errorf("%v", err)
	}
}

var _ = json.Marshal
