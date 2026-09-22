package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

var (
	runInput     string
	runInputFile string
	runChat      bool
	runSets      []string
	runSlots     []string
	runVars      string
	runProfile   string
	runVariant   string
	runOverlays  []string
	runEnv       string
	runWith      []string
	runContext   string
	runModel     string
	runMaxTokens int
	runMaxTurns  int
	runNoStream  bool
	runCheck     bool
	runOut       string
	runJSON      bool
	runDryRun    bool
)

var runCmd = &cobra.Command{
	Use:   "run <PromptName>",
	Short: "Run a prompt against a model and stream the answer",
	Long: `Resolve a prompt exactly as 'loom weave' would, send it to a model as the system message,
and stream the answer to the terminal.

  loom run CodeReviewer --input-file change.patch
  git diff | loom run CodeReviewer --with file:CONTRIBUTING.md
  loom run Tutor --chat                     # a conversation; /exit, /reset, /show, Ctrl-C stops a reply
  loom run CodeReviewer --input-file x.go --check       # exit 1 if the answer breaks the contract
  loom run CodeReviewer --dry-run --input-file x.go     # show exactly what would be sent; call nothing

The provider, key and model come from [testing] in loom.toml; --model overrides the model for this
run ("model" or "provider:model"). The model can only produce text: loom never executes an answer
or acts on it. Replies printed to a terminal have control sequences stripped.

What may be attached with --with / --context is limited by "permission.read" in .loom.config,
and where --out may write by "permission.write". See docs/AGENT_RUNTIME.md.`,
	Args: cobra.ExactArgs(1),
	RunE: runRunCmd,
}

func init() {
	f := runCmd.Flags()
	f.StringVarP(&runInput, "input", "i", "", "the message to send (default: stdin when piped, else just the prompt)")
	f.StringVar(&runInputFile, "input-file", "", "read the message from a file")
	f.BoolVar(&runChat, "chat", false, "open a conversation (the history is kept and sent with each message)")
	f.StringArrayVar(&runSets, "set", nil, "set a render variable (key=value)")
	f.StringArrayVar(&runSlots, "slot", nil, "set a slot value (alias for --set)")
	f.StringVar(&runVars, "vars", "", "load render variables from a TOML file")
	f.StringVar(&runProfile, "profile", "", "load a named profile from loom.toml")
	f.StringVar(&runVariant, "variant", "", "apply a named prompt variant")
	f.StringArrayVar(&runOverlays, "overlay", nil, "apply one or more overlays by name")
	f.StringVar(&runEnv, "env", "", "apply an env block (e.g. prod, dev)")
	f.StringArrayVar(&runWith, "with", nil, "attach context: file:path, dir:path, git:diff, git:staged, stdin")
	f.StringVar(&runContext, "context", "", "load a named context bundle from contexts/<name>.context")
	f.StringVar(&runModel, "model", "", "model to use: model or provider:model (default: [testing] in loom.toml)")
	f.IntVar(&runMaxTokens, "max-tokens", 0, "limit the length of each answer")
	f.IntVar(&runMaxTurns, "max-turns", agent.DefaultMaxTurns, "with --chat: the most exchanges kept in one conversation")
	f.BoolVar(&runNoStream, "no-stream", false, "wait for the whole answer instead of streaming it")
	f.BoolVar(&runCheck, "check", false, "check answers against the prompt's contract; exit 1 on a violation")
	f.StringVar(&runOut, "out", "", "write a Markdown transcript to this file (needs permission.write)")
	f.BoolVar(&runJSON, "json", false, "print one JSON object instead of the answer text (not with --chat)")
	f.BoolVar(&runDryRun, "dry-run", false, "print what would be sent and call nothing")
}

// runParams is everything the command needs, so it can be driven from tests.
type runParams struct {
	Name  string
	Cwd   string
	Weave tui.WeaveOptions

	Input, InputFile string
	Chat             bool
	Model            string
	MaxTokens        int
	MaxTurns         int
	NoStream         bool
	Check            bool
	Out              string
	JSON             bool
	DryRun           bool

	Stdin            io.Reader
	Stdout, Stderr   io.Writer
	StdinTTY, OutTTY bool

	// NewModel builds the client; tests replace it.
	NewModel func(cfg *config.Config, provider, model string) (agent.Model, error)
	// TurnContext returns the context of one request; Ctrl-C cancels it.
	TurnContext func() (context.Context, context.CancelFunc)
}

// defaultTask is sent when a prompt needs no input of its own.
const defaultTask = "Follow your instructions."

func runRunCmd(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	sets := append(append([]string{}, runSets...), runSlots...)
	vars, err := tui.ParseKVArgs(sets)
	if err != nil {
		return err
	}
	varsFile := runVars
	if varsFile != "" && !filepath.IsAbs(varsFile) {
		varsFile = filepath.Join(cwd, varsFile)
	}
	fileVars, err := tui.LoadVarsFile(varsFile)
	if err != nil {
		return err
	}
	for k, v := range vars {
		fileVars[k] = v
	}
	return executeRun(runParams{
		Name: args[0], Cwd: cwd,
		Weave: tui.WeaveOptions{
			Variables: fileVars, Profile: runProfile, Variant: runVariant, Overlays: runOverlays,
			Env: runEnv, WithSources: runWith, ContextBundle: runContext,
		},
		Input: runInput, InputFile: runInputFile, Chat: runChat, Model: runModel,
		MaxTokens: runMaxTokens, MaxTurns: runMaxTurns, NoStream: runNoStream, Check: runCheck,
		Out: runOut, JSON: runJSON, DryRun: runDryRun,
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		StdinTTY: isatty.IsTerminal(os.Stdin.Fd()), OutTTY: isatty.IsTerminal(os.Stdout.Fd()),
	})
}

func executeRun(p runParams) error {
	if p.JSON && p.Chat {
		return fmt.Errorf("--json cannot be combined with --chat")
	}
	if p.NewModel == nil {
		p.NewModel = func(cfg *config.Config, provider, model string) (agent.Model, error) {
			c, err := llm.New(cfg, provider, model)
			if err != nil {
				return nil, err
			}
			if c.Timeout == 0 {
				c.Timeout = 60 * time.Second
			}
			usage.Attach(c, usage.Open(usage.DefaultPath(p.Cwd)), cfg, "run", "")
			return c, nil
		}
	}
	if p.TurnContext == nil {
		p.TurnContext = func() (context.Context, context.CancelFunc) {
			return signal.NotifyContext(context.Background(), os.Interrupt)
		}
	}

	// 1. What may be read and written: checked before anything is.
	perm, err := agent.LoadPermission(p.Cwd)
	if err != nil {
		return err
	}
	if err := perm.CheckSources(p.Cwd, p.Weave.WithSources, p.Weave.ContextBundle); err != nil {
		return err
	}
	if p.Out != "" {
		if err := perm.CheckWrite(absIn(p.Cwd, p.Out)); err != nil {
			return fmt.Errorf("--out %s: %w", p.Out, err)
		}
	}

	// 2. The prompt, resolved and rendered like `loom weave`.
	prep, err := tui.PrepareRun(p.Name, p.Weave, p.Cwd)
	if err != nil {
		return err
	}

	// 3. The first message.
	input, err := readRunInput(p, perm)
	if err != nil {
		return err
	}

	provider, model := llm.ParseSpec(p.Model)
	prov, mdl, _, err := llm.Resolve(prep.Config, provider, model)
	if err != nil {
		return err
	}
	label := prov + ":" + mdl

	if p.DryRun {
		return printDryRun(p, prep.Body, input, label)
	}

	client, err := p.NewModel(prep.Config, provider, model)
	if err != nil {
		return err
	}
	sess := &agent.Session{
		Model: client, System: prep.Body, Contract: prep.Contract,
		MaxTokens: p.MaxTokens, MaxTurns: p.MaxTurns, Stream: !p.NoStream || p.JSON,
	}
	tr := &transcript{name: p.Name, model: label, system: prep.Body}

	var runErr error
	if p.Chat {
		runErr = runChatLoop(p, sess, tr, input)
	} else {
		if input == "" {
			input = defaultTask
		}
		runErr = runOnce(p, sess, tr, input)
	}

	if p.Out != "" && len(tr.turns) > 0 {
		if err := tr.write(absIn(p.Cwd, p.Out)); err != nil && runErr == nil {
			runErr = err
		} else if err == nil && p.OutTTY {
			fmt.Fprintf(p.Stderr, "  transcript → %s\n", p.Out)
		}
	}
	return runErr
}

func absIn(cwd, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(cwd, path)
}

// readRunInput returns the first message from --input, --input-file or piped stdin.
func readRunInput(p runParams, perm *agent.Permission) (string, error) {
	switch {
	case p.Input != "" && p.InputFile != "":
		return "", fmt.Errorf("use --input or --input-file, not both")
	case p.Input != "":
		return p.Input, nil
	case p.InputFile != "":
		path := absIn(p.Cwd, p.InputFile)
		if err := perm.CheckRead(path); err != nil {
			return "", fmt.Errorf("--input-file %s: %w", p.InputFile, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("--input-file: %w", err)
		}
		return string(data), nil
	case !p.Chat && !p.StdinTTY && p.Stdin != nil:
		data, err := io.ReadAll(io.LimitReader(p.Stdin, 8<<20))
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(data), nil
	}
	return "", nil
}

func printDryRun(p runParams, system, input, label string) error {
	if input == "" {
		input = defaultTask
	}
	w := p.Stdout
	fmt.Fprintf(w, "model: %s\n", label)
	if p.MaxTokens > 0 {
		fmt.Fprintf(w, "max tokens: %d\n", p.MaxTokens)
	}
	fmt.Fprintf(w, "\n── system prompt (%d characters) ──\n%s\n", len(system), strings.TrimRight(system, "\n"))
	if p.Chat {
		fmt.Fprint(w, "\n── conversation ──\n(you type the messages; the system prompt above is sent with every one)\n")
		if p.Input != "" || p.InputFile != "" {
			fmt.Fprintf(w, "\n── first message ──\n%s\n", strings.TrimRight(input, "\n"))
		}
	} else {
		fmt.Fprintf(w, "\n── message ──\n%s\n", strings.TrimRight(input, "\n"))
	}
	fmt.Fprint(w, "\n(dry run: nothing was sent)\n")
	return nil
}

// ── one exchange ─────────────────────────────────────────────────────────────

func runOnce(p runParams, sess *agent.Session, tr *transcript, input string) error {
	reply, err := sendAndPrint(p, sess, input, !p.JSON)
	if reply.Text != "" || err == nil {
		tr.add(input, reply.Text)
	}
	if err != nil {
		return describeRunError(p, err)
	}
	if p.JSON {
		return printRunJSON(p, sess, tr, input, reply)
	}
	printFooter(p, reply)
	return checkContract(p, reply)
}

// sendAndPrint sends one message and prints the answer as it streams in.
func sendAndPrint(p runParams, sess *agent.Session, input string, print bool) (agent.Reply, error) {
	ctx, stop := p.TurnContext()
	defer stop()

	var san agent.Sanitizer
	var last string
	onDelta := func(d string) {
		if !print {
			return
		}
		if p.OutTTY {
			d = san.Filter(d) // an answer is untrusted text; a terminal must not obey its escape codes
		}
		if d != "" {
			last = d
			fmt.Fprint(p.Stdout, d)
		}
	}
	reply, err := sess.Send(ctx, input, onDelta)
	if print && p.OutTTY && last != "" && !strings.HasSuffix(last, "\n") {
		fmt.Fprintln(p.Stdout)
	}
	return reply, err
}

func describeRunError(p runParams, err error) error {
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(p.Stderr, "\n(interrupted)")
		return errors.New("interrupted")
	}
	return err
}

func printFooter(p runParams, r agent.Reply) {
	if !p.OutTTY {
		return
	}
	parts := []string{r.Duration.Round(10 * time.Millisecond).String()}
	if r.Usage.InputTokens > 0 || r.Usage.OutputTokens > 0 {
		parts = append(parts, fmt.Sprintf("%d in / %d out tokens", r.Usage.InputTokens, r.Usage.OutputTokens))
	}
	fmt.Fprintf(p.Stderr, "  · %s\n", strings.Join(parts, " · "))
}

func checkContract(p runParams, r agent.Reply) error {
	if !p.Check || len(r.ContractFailures) == 0 {
		return nil
	}
	for _, f := range r.ContractFailures {
		fmt.Fprintf(p.Stderr, "  ✗ contract: %s\n", f.Detail)
	}
	return fmt.Errorf("the answer violates the prompt's contract (%d problem(s))", len(r.ContractFailures))
}

// ── conversation ─────────────────────────────────────────────────────────────

func runChatLoop(p runParams, sess *agent.Session, tr *transcript, first string) error {
	violations := 0
	turn := func(msg string) error {
		reply, err := sendAndPrint(p, sess, msg, true)
		if reply.Text != "" || err == nil {
			tr.add(msg, reply.Text)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(p.Stderr, "\n(interrupted; the message was not added to the conversation)")
				return nil
			}
			return err
		}
		printFooter(p, reply)
		if p.Check && len(reply.ContractFailures) > 0 {
			violations++
			for _, f := range reply.ContractFailures {
				fmt.Fprintf(p.Stderr, "  ✗ contract: %s\n", f.Detail)
			}
		}
		return nil
	}

	if first != "" {
		if err := turn(first); err != nil {
			return err
		}
	}
	if p.StdinTTY {
		fmt.Fprintln(p.Stderr, "  chat: /exit to leave, /reset to start over, /show for the system prompt, Ctrl-C stops a reply")
	}
	sc := bufio.NewScanner(p.Stdin)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for {
		if p.StdinTTY {
			fmt.Fprint(p.Stderr, "you> ")
		}
		if !sc.Scan() {
			break
		}
		line := strings.TrimSpace(sc.Text())
		switch line {
		case "":
			continue
		case "/exit", "/quit":
			return finishChat(p, violations)
		case "/reset":
			sess.Reset()
			fmt.Fprintln(p.Stderr, "  (conversation cleared)")
			continue
		case "/show":
			fmt.Fprintf(p.Stderr, "%s\n", sess.System)
			continue
		}
		if err := turn(line); err != nil {
			fmt.Fprintf(p.Stderr, "  ✗ %v\n", err)
			if strings.Contains(err.Error(), "reached") && strings.Contains(err.Error(), "turns") {
				continue // the limit message tells the user to /reset
			}
			return err
		}
	}
	return finishChat(p, violations)
}

func finishChat(p runParams, violations int) error {
	if violations > 0 {
		return fmt.Errorf("%d answer(s) violated the prompt's contract", violations)
	}
	return nil
}

// ── output ───────────────────────────────────────────────────────────────────

func printRunJSON(p runParams, sess *agent.Session, tr *transcript, input string, r agent.Reply) error {
	out := map[string]any{
		"prompt": p.Name, "model": tr.model, "input": input, "output": r.Text,
		"usage":       map[string]int{"input_tokens": r.Usage.InputTokens, "output_tokens": r.Usage.OutputTokens},
		"duration_ms": r.Duration.Milliseconds(),
	}
	var failures []string
	for _, f := range r.ContractFailures {
		failures = append(failures, f.Detail)
	}
	out["contract_failures"] = append([]string{}, failures...)
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return err
	}
	if p.Check && len(failures) > 0 {
		return fmt.Errorf("the answer violates the prompt's contract (%d problem(s))", len(failures))
	}
	return nil
}

// transcript records a run for --out.
type transcript struct {
	name, model, system string
	turns               [][2]string
}

func (t *transcript) add(user, assistant string) {
	t.turns = append(t.turns, [2]string{user, assistant})
}

func (t *transcript) write(path string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# loom run: %s\n\n- model: %s\n\n## System\n\n%s\n", t.name, t.model, strings.TrimRight(t.system, "\n"))
	for _, turn := range t.turns {
		fmt.Fprintf(&b, "\n## You\n\n%s\n\n## Assistant\n\n%s\n", strings.TrimRight(turn[0], "\n"), strings.TrimRight(turn[1], "\n"))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 0600: a transcript holds whatever was sent and received.
	return os.WriteFile(path, []byte(b.String()), 0o600)
}
