# Agent runtime: design

Status: **accepted** (PL-501). Implemented by `loom run` (PL-502). Builds on the shared model client
(`internal/llm`, PL-405). Extended by refine/decide (PL-503, `loom optimize`/`loom score`/`loom eval
--refine`), quest mode (PL-504, `loom quest run`/`loom quest list`) and loom scripts (PL-505,
`loom script run`/`loom script list`).

## Goal

Run a prompt you have written, resolved and validated with loom **against a model**, from the
terminal, and see the answer as it is produced: `loom run CodeReviewer --input-file diff.patch`.
A prompt library is only useful if the prompts can be exercised; `run` closes the loop between
*writing* a prompt (`weave`, `inspect`) and *judging* it (`test`, `eval`).

## Scope

| Capability | Version 1 | Why |
|---|---|---|
| Resolve + render a prompt exactly like `weave` (vars, slots, variant, overlays, env, `--with` context) | **Yes** | one meaning of "a prompt": `run` and `weave` share `renderSingle` |
| Streaming output | **Yes** | a long answer is useless if you wait for all of it; Ctrl-C must be able to stop a bad one |
| Multi-turn (`--chat`) | **Yes** | most prompt debugging is a conversation; history is local and explicit |
| Contract check of the answer (`--check`) | **Yes** | the contract already exists; `run` can enforce it |
| Token usage | best effort | providers report it; PL-704 will aggregate |
| **Tool use / function calling** | **No** | see the safety model: the model can only produce text, so there is nothing to authorize |
| Model-driven file writes, shell commands, web access | **No** | same |
| Autonomous multi-step loops | **No** | `loom quest` (PL-504) composes a *fixed, human-authored* sequence of `run` steps — never a model-chosen one |

Non-goals: being a general chat client, managing conversations across sessions (a transcript can be
saved, not resumed), or picking a model for you.

## Safety model

`loom run` sends text to a third party and prints text back. There are four things to get right.

### 1. The model can act on nothing

In version 1 the model's reply is **data**. `loom` never executes it, writes files because of it, or
calls tools on its behalf. This is deliberate: it makes the runtime safe by construction and lets
the rest of the model be simple. When tool use arrives it must satisfy the conditions in
*Future: tools* below; it will not be added by loosening anything here.

### 2. What leaves the machine

Everything sent is visible **before** it is sent (`--dry-run` prints the exact system prompt, the
history and the input, and calls nothing). What can be included, and the guards on each:

| Content | Guard |
|---|---|
| The rendered prompt | secret slots (`slot x { secret: true }`) refuse plain `--set` values and are never echoed |
| `--with file:PATH`, `dir:PATH`, `--context` bundles | `.loom.config` **`permission.read`** patterns decide which paths may be read (default `*`); directory and bundle sources already skip credential files (`.env`, `*.pem`, `.loomsecret`, …); an explicit `file:` naming a credential file is refused |
| The user's input | the user's own text |
| `git:` and `stdin` sources | the user's own choice; shown by `--dry-run` |

`permission.read` is the first place that setting is enforced. It lets a project state once, in a
file an agent cannot quietly rewrite while locked, which files may be shown to a model.

### 3. What comes back

A reply is untrusted text. Two consequences:

- **Terminal safety.** A reply printed to a terminal is stripped of control characters (ANSI/OSC
  escape sequences can retitle the window, move the cursor to hide text, or write to the
  clipboard). Newlines and tabs are kept. When stdout is a pipe or file the reply is passed through
  unchanged, because the consumer is a program, not a terminal.
- **No execution.** See 1.

### 4. Secrets and LoomLocker

- The **API key** is read from the environment or `.loomsecret` (PL-405) and only ever sent in a
  header. If LoomLocker has the key file locked, the "key" is a token (`lk_…`) and the provider
  would answer with an authentication error; `run` recognises the token and says to run under
  `loom execute <cmd> --unlock` (or unlock first) instead of leaking a confusing 401.
- A **transcript** (`--out`) may contain whatever was sent and received. Writing it obeys
  **`permission.write`**, and it never contains the API key.

### Limits

`--max-tokens` bounds the answer; attached context is size-capped (the existing bundle limits);
requests time out (`[testing] timeout_sec`, chat turns get their own timeout); **Ctrl-C** cancels the
request in flight and, in chat, returns to the prompt rather than exiting; a chat keeps at most
`--max-turns` (default 50) exchanges so an accidental loop cannot grow without bound.

## Interface

```
loom run <Name> [--input TEXT | --input-file PATH | (stdin)] [--chat]
         [--set k=v] [--vars FILE] [--profile P] [--variant V] [--overlay O] [--env E]
         [--with SRC] [--context BUNDLE]
         [--model [provider:]model] [--max-tokens N] [--no-stream]
         [--check] [--out FILE] [--json] [--dry-run]
```

- Without `--chat`, the input is the one user message; with none given and stdin a terminal, the
  prompt alone is sent (some prompts need no input).
- `--chat` opens a conversation: your lines are messages, `/exit` (or Ctrl-D) ends, `/reset` clears
  the history, `/show` prints the system prompt. The rendered prompt is the **system** message of
  every turn.
- Exit codes: `0` success; `1` a failure (model error, refused context, missing key, bad flags), or
  with `--check` the answer violated the prompt's contract.

## Architecture

```
loom run
  └─ tui.renderSingle          resolve + render (shared with weave)  → system text, sources
  └─ run.Check permissions     permission.read / write
  └─ agent.Session             history, turn limit, contract check
       └─ llm.Client.Stream    provider streaming (SSE) → deltas
```

`internal/llm` gains `Stream` (Gemini `streamGenerateContent?alt=sse`, Anthropic `stream: true`,
OpenAI `stream: true`), a conversation history on `Request`, and best-effort `Usage`. Streaming
parsers are tested against recorded provider events, not live services.

## Exception: `loom optimize`

`loom optimize` (PL-503) is the one place a model's output changes a project file, so it is worth
being explicit about why that does not contradict rule 1 above.

- It is **human-invoked**, never something another command triggers on its own.
- It can touch exactly **one thing**: the *field content* of the one named prompt. The refiner's
  reply is parsed with the real DSL parser, and a proposal that changes the prompt's name,
  `inherits` list, `use` lines, `var`/`slot` declarations, `variant`/`env` blocks, or
  `contract`/`capabilities` block is rejected outright — never partially applied. The write itself
  goes through `format.ReplaceFields`, which only ever copies structure from the *original* node,
  so even a caller bug cannot smuggle a structural change through it.
- The **diff is always shown**. Without `--yes` nothing is written at all — a preview.
- Every write still goes through **`permission.write`** in `.loom.config`, the same as any other
  file loom writes.
- Success is judged by the prompt's own **eval suite**, written by a human before `optimize` ever
  runs. An applied change that scores worse is reverted immediately, and a run is bounded
  (`--iterations`, default 3).

This is a narrow, auditable exception — "rewrite this one prompt's wording, show me first" — not a
step toward the model executing arbitrary actions, which is still out of scope (see below).

## `loom quest`: not an exception

`loom quest run` (PL-504) executes a `.quest.toml` file: a fixed, human-authored list of `run`
steps, each naming its own prompt. The only thing that moves between steps is text — a step's
`input` may reference `{{quest.input}}` and `{{quest.previous}}`, substituted before the step is
sent, nothing more. Which prompt runs, in what order, and with what permissions is fixed by the
file on disk, never chosen by a model while the quest runs. So none of the four points in the
safety model above are relaxed: a quest can act on nothing a `loom run` of the same prompt
couldn't, and it writes a file only when `--out` is given, gated by `permission.write`, exactly as
`loom run --out` already is.

## `loom script`: not a new capability

`loom script run` (PL-505) executes a `.lmscr` file: a fixed, human-authored list of steps, each
naming one loom (sub)command and its arguments (`weave`, `eval`, `optimize --yes`, `quest run`,
`deploy`, anything else loom can do). Each step is started exactly as if it had been typed by
hand — the loom binary itself, never a shell, with the step's `run` and (variable-substituted)
`args` passed straight through as argv, so there is no shell injection surface and no way for a
step's own output to be interpreted as more commands.

A script does not add a capability; it automates typing a sequence of commands that already exist,
each of which enforces its own safety on its own terms exactly as it would run alone: a
`optimize --yes` step still needs `permission.write` and still shows its diff on the way; a `deploy`
step still writes only its configured targets; a `run` step is still text-only, no tool use. Which
commands run, in what order, and with what arguments is fixed by the file on disk — a script never
lets one step's *output* decide what the next step is (the only per-step branching is `when`, which
looks at whether the *previous step succeeded*, not at anything it said).

## Future: tools (not in version 1)

A tool-calling runtime will only be built when all of these can be met:

1. **Opt-in per tool, per prompt.** A tool is available only if the prompt's `capabilities` block
   names it in `allowed`; `forbidden` always wins, including over inherited `allowed`.
2. **Least privilege on paths.** File tools obey `permission.read` / `permission.write`.
3. **Confirmation by default.** Every call that changes state (write, exec, network) is shown and
   needs an explicit yes, unless the user passed a flag naming exactly what may run unattended.
4. **Audit.** Every call and result is recorded in the transcript.
5. **LoomLocker aware.** Tools never see secrets that are locked, and their output is scanned for
   the tokens/values of configured secrets before it is shown or sent back.

## Testing

Provider streams (well-formed, split mid-event, error events, stray blank lines) against fake
servers; history and turn limits; permissions; terminal sanitising; `--dry-run` making no request;
Ctrl-C/context cancellation; end-to-end through the binary with a fake endpoint.
