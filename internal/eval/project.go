package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

// Params is what `loom eval` (and the loom ci gate) asks for.
type Params struct {
	Names     []string // suite or prompt names; empty = every suite
	Models    []string // "model" or "provider:model"; empty = the model in [testing]
	Judge     string   // same syntax; empty = the suite's judge_model, else the model in [testing]
	Record    bool
	Compare   bool
	Tolerance int // points; <= 0 means DefaultTolerance (use Strict for 0)
	Strict    bool
	Threshold int    // overrides every pass mark when > 0
	Dir       string // suites directory; empty = <project>/evals

	// ClientFor builds a client for a provider ("" = the project's) and model ("" = its default).
	// Tests replace it; the default is llm.New.
	ClientFor func(cfg *config.Config, provider, model string) (Completer, string, error)

	// Command labels usage records ("eval", "score", "optimize"); empty means "eval". UsageLog
	// records token/cost history for every model this run calls (the model(s) under test with role
	// "", the judge with role "judge"); nil means <project>/.loom/usage.jsonl, the same ledger `loom
	// usage` reads. Recording is best effort and never affects the run itself.
	Command  string
	UsageLog *usage.Log
}

// SuiteOutcome is the result of one suite.
type SuiteOutcome struct {
	Suite       *Suite
	Results     []CaseResult
	Comparisons []Comparison
	NoBaseline  bool // --compare was asked for but nothing was recorded yet
	Recorded    int
}

// Outcome is everything a run produced.
type Outcome struct {
	Suites  []SuiteOutcome
	Summary Summary
	// Regressions counts scores that dropped past the tolerance (only with Compare).
	Regressions int
}

// OK reports whether the run should exit 0: every case passed and nothing regressed.
func (o *Outcome) OK() bool { return o.Summary.OK() && o.Regressions == 0 }

func defaultClientFor(cfg *config.Config, provider, model string) (Completer, string, error) {
	c, err := llm.New(cfg, provider, model)
	if err != nil {
		return nil, "", err
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return c, c.Model, nil
}

// RunProject loads the suites of the project at cwd and runs them.
func RunProject(ctx context.Context, cwd string, p Params) (*Outcome, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return nil, err
	}
	dir := p.Dir
	if dir == "" {
		dir = filepath.Join(cwd, DefaultDir)
	} else if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	clientFor := p.ClientFor
	if clientFor == nil {
		clientFor = defaultClientFor
	}
	command := p.Command
	if command == "" {
		command = "eval"
	}
	log := p.UsageLog
	if log == nil {
		log = usage.Open(usage.DefaultPath(cwd))
	}

	suites, err := selectSuites(dir, p.Names)
	if err != nil {
		return nil, err
	}
	if len(suites) == 0 {
		return nil, fmt.Errorf("no eval suites found in %s (create evals/<Name>.eval.toml; see `loom eval --help`)", dir)
	}

	// models under test
	specs := p.Models
	if len(specs) == 0 {
		specs = []string{""}
	}
	var models []Model
	for _, spec := range specs {
		prov, name := llm.ParseSpec(spec)
		client, label, err := clientFor(cfg, prov, name)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", spec, err)
		}
		usage.Attach(client, log, cfg, command, "")
		if spec != "" && prov != "" {
			label = prov + ":" + label
		}
		models = append(models, Model{Label: label, Client: client})
	}

	tolerance := p.Tolerance
	if tolerance <= 0 && !p.Strict {
		tolerance = DefaultTolerance
	}

	out := &Outcome{}
	var all []CaseResult
	for _, s := range suites {
		judgeSpec := p.Judge
		if judgeSpec == "" {
			judgeSpec = s.JudgeModel
		}
		jp, jm := llm.ParseSpec(judgeSpec)
		judge, _, err := clientFor(cfg, jp, jm)
		if err != nil {
			return nil, fmt.Errorf("judge: %w", err)
		}
		usage.Attach(judge, log, cfg, command, "judge")

		so := SuiteOutcome{Suite: s}
		so.Results = RunSuite(ctx, reg, cfg, s, Options{Models: models, Judge: judge, Threshold: p.Threshold})

		if p.Compare {
			base, err := LoadBaseline(dir, s.Name)
			if err != nil {
				return nil, err
			}
			so.NoBaseline = base == nil
			so.Comparisons = Compare(base, so.Results, tolerance)
			out.Regressions += Regressions(so.Comparisons)
		}
		if p.Record {
			if Summarize(so.Results).Errored > 0 {
				return nil, fmt.Errorf("not recording %s: some cases failed to run, and a baseline with holes would hide them (fix the errors, then record again)", s.Name)
			}
			n, err := Record(dir, s, so.Results)
			if err != nil {
				return nil, err
			}
			so.Recorded = n
		}
		out.Suites = append(out.Suites, so)
		all = append(all, so.Results...)
	}
	out.Summary = Summarize(all)
	return out, nil
}

// selectSuites loads the suites named (by suite or prompt name), or all of them.
func selectSuites(dir string, names []string) ([]*Suite, error) {
	paths, err := FindSuites(dir)
	if err != nil {
		return nil, err
	}
	var suites []*Suite
	for _, path := range paths {
		s, err := LoadSuite(path)
		if err != nil {
			return nil, err
		}
		suites = append(suites, s)
	}
	if len(names) == 0 {
		return suites, nil
	}
	var picked []*Suite
	for _, n := range names {
		found := false
		for _, s := range suites {
			if s.Name == n || s.Prompt == n {
				picked = append(picked, s)
				found = true
			}
		}
		if !found {
			var have []string
			for _, s := range suites {
				have = append(have, s.Name)
			}
			return nil, fmt.Errorf("no eval suite for %q (suites: %s)", n, strings.Join(have, ", "))
		}
	}
	return picked, nil
}

// Text renders the whole outcome for a terminal.
func (o *Outcome) Text() string {
	var b strings.Builder
	for _, so := range o.Suites {
		b.WriteString(Report(so.Suite, so.Results, so.Comparisons))
		switch {
		case so.NoBaseline:
			b.WriteString("  no baseline yet: run `loom eval --record` to save these scores\n")
		case so.Recorded > 0:
			fmt.Fprintf(&b, "  recorded %d score(s) to %s\n", so.Recorded, filepath.ToSlash(filepath.Join(DefaultDir, BaselineDir, so.Suite.Name+".json")))
		}
		b.WriteByte('\n')
	}
	s := o.Summary
	fmt.Fprintf(&b, "%d case(s): %d passed, %d failed, %d errored", s.Total, s.Passed, s.Failed, s.Errored)
	if o.Regressions > 0 {
		fmt.Fprintf(&b, ", %d regressed against the baseline", o.Regressions)
	}
	b.WriteByte('\n')
	return b.String()
}
