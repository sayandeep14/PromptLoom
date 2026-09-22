// Package usage records what loom's model calls cost, in tokens and (when the project has told
// loom the price of a model) in dollars. It never invents a price: a model with no matching entry
// in loom.toml's [[pricing]] shows token counts only, never a guessed cost. See `loom usage` and
// `loom bench`, and docs/AGENT_RUNTIME.md's "Token usage" note.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

// DefaultDir is where the usage ledger lives, relative to the project. It is not meant to be
// committed (loom init adds it to .gitignore): it is a personal, local record of what ran on this
// machine, not shared project state.
const DefaultDir = ".loom"

// DefaultFile is the ledger's file name inside DefaultDir.
const DefaultFile = "usage.jsonl"

// DefaultPath returns the ledger path for a project at cwd.
func DefaultPath(cwd string) string {
	return filepath.Join(cwd, DefaultDir, DefaultFile)
}

// Record is one model call. The ledger is append-only — a Record, once written, is never edited.
type Record struct {
	Time     time.Time `json:"time"`
	Command  string    `json:"command"`        // "run", "quest run", "eval", "optimize", "score", "bench", ...
	Role     string    `json:"role,omitempty"` // "" (the primary call), "judge", "refiner"
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Input    int       `json:"input_tokens"`
	Output   int       `json:"output_tokens"`
	CostUSD  float64   `json:"cost_usd,omitempty"`
	HasCost  bool      `json:"has_cost"` // false: no [[pricing]] entry matched; CostUSD is always 0, never guessed
}

// EstimateCost looks up provider+model in cfg's [[pricing]] (case-insensitive) and prices u
// against it. ok is false, and cost is always 0, when nothing matches — never an invented number.
func EstimateCost(cfg *config.Config, provider, model string, u llm.Usage) (cost float64, ok bool) {
	if cfg == nil {
		return 0, false
	}
	for _, p := range cfg.Pricing {
		if strings.EqualFold(p.Provider, provider) && strings.EqualFold(p.Model, model) {
			return float64(u.InputTokens)/1_000_000*p.Input + float64(u.OutputTokens)/1_000_000*p.Output, true
		}
	}
	return 0, false
}

// Log is an append-only ledger at Path.
type Log struct {
	Path string
}

// Open returns a Log at path; nothing is read or created until the first Append.
func Open(path string) *Log { return &Log{Path: path} }

// Append writes one Record. A nil Log is a no-op — usage recording is best effort and must never
// be the reason a model call fails.
func (l *Log) Append(rec Record) error {
	if l == nil || l.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}

// ReadAll returns every Record in the ledger at path, oldest first. A missing ledger is empty, not
// an error — nothing has run yet. A line that fails to parse is skipped, not fatal: a partial
// write (e.g. a killed process) must not make the rest of the history unreadable.
func ReadAll(path string) ([]Record, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// Attach points c at log, so every successful call it makes is recorded under command/role, with a
// cost estimated from cfg's [[pricing]] when a rate is known. c may be a fake (a test's stand-in
// for agent.Model/eval.Completer, not a real *llm.Client) — Attach then silently does nothing,
// since usage recording is an observability add-on, never a reason a fake stops working. cfg may
// be nil (cost is then never estimated); log may be nil (nothing is ever recorded).
func Attach(c any, log *Log, cfg *config.Config, command, role string) {
	lc, ok := c.(*llm.Client)
	if !ok || log == nil {
		return
	}
	provider, model := lc.Provider, lc.Model
	lc.OnUsage = func(u llm.Usage) {
		rec := Record{
			Time: time.Now(), Command: command, Role: role, Provider: provider, Model: model,
			Input: u.InputTokens, Output: u.OutputTokens,
		}
		if cost, ok := EstimateCost(cfg, provider, model, u); ok {
			rec.CostUSD, rec.HasCost = cost, true
		}
		log.Append(rec)
	}
}
