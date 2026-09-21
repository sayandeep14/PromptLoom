package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/contract"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

// Model is what a Session needs from a model client (llm.Client implements it).
type Model interface {
	Stream(ctx context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error)
	Complete(ctx context.Context, r llm.Request) (string, error)
}

// DefaultMaxTurns bounds a conversation so an accidental loop cannot grow without limit.
const DefaultMaxTurns = 50

// Session is a conversation with a model. The rendered prompt is the system message of every turn;
// the history is kept locally and sent with each new message.
type Session struct {
	Model     Model
	System    string
	Contract  *ast.ContractBlock // checked against every reply when set
	MaxTokens int
	MaxTurns  int  // 0 means DefaultMaxTurns
	Stream    bool // stream deltas (otherwise one call, and onDelta gets the whole reply)

	history []llm.Message
}

// Reply is the outcome of one turn.
type Reply struct {
	Text             string
	Usage            llm.Usage
	ContractFailures []contract.Failure
	Duration         time.Duration
	// Partial is true when the reply was cut short (cancelled or failed mid-stream); Text holds
	// what arrived, but the turn is NOT added to the history.
	Partial bool
}

// Turns is the number of completed exchanges.
func (s *Session) Turns() int { return len(s.history) / 2 }

// History returns a copy of the conversation so far.
func (s *Session) History() []llm.Message { return append([]llm.Message(nil), s.history...) }

// Reset forgets the conversation (the system prompt stays).
func (s *Session) Reset() { s.history = nil }

// Request is exactly what the next Send(user) would put on the wire; --dry-run prints it.
func (s *Session) Request(user string) llm.Request {
	return llm.Request{System: s.System, History: s.History(), User: user, MaxTokens: s.MaxTokens}
}

// Send sends one user message and returns the reply. onDelta (may be nil) receives the answer
// as it arrives. A failed or cancelled turn leaves the history as it was, so the same message can
// be sent again.
func (s *Session) Send(ctx context.Context, user string, onDelta func(string)) (Reply, error) {
	limit := s.MaxTurns
	if limit <= 0 {
		limit = DefaultMaxTurns
	}
	if s.Turns() >= limit {
		return Reply{}, fmt.Errorf("the conversation reached %d turns; start a new one (/reset)", limit)
	}

	start := time.Now()
	req := s.Request(user)
	var (
		text  string
		usage llm.Usage
		err   error
	)
	if s.Stream {
		text, usage, err = s.Model.Stream(ctx, req, onDelta)
	} else {
		text, err = s.Model.Complete(ctx, req)
		if err == nil && onDelta != nil {
			onDelta(text)
		}
	}
	reply := Reply{Text: text, Usage: usage, Duration: time.Since(start)}
	if err != nil {
		reply.Partial = text != ""
		return reply, err
	}
	s.history = append(s.history, llm.Message{Role: "user", Content: user}, llm.Message{Role: "assistant", Content: text})
	reply.ContractFailures = contract.Check(s.Contract, text)
	return reply, nil
}
