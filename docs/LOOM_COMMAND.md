# Loom CLI — Complete Command Reference

> **For:** developers using the `loom` CLI to build, validate, render, and manage prompt libraries  
> **Version:** v2 (current)  
> **Companion doc:** `docs/LOOM_LANGUAGE.md` — the DSL syntax reference

---

## How to read this document

Every command entry follows this structure:

- **What it does** — plain description
- **When to use it** — the right moment to reach for this command
- **Why it exists** — the problem it solves
- **Syntax** — the full command form
- **Flags** — every flag with an explanation
- **Examples** — real, copy-pasteable invocations

Global flag available on every command:

```
--theme light|dark    Force a colour theme for terminal output
```

---

## Table of Contents

| Category | Commands |
|---|---|
| [Project Setup](#project-setup) | `init`, `start`, `thread` |
| [Rendering](#rendering) | `weave`, `cast`, `copy` |
| [Validation & Inspection](#validation--inspection) | `inspect`, `trace`, `unravel`, `contract` |
| [Code Quality](#code-quality) | `doctor`, `smells`, `stats`, `minimize`, `audit` |
| [Git & History](#git--history) | `blame`, `changelog`, `diff`, `review` |
| [CI & Locking](#ci--locking) | `ci`, `lock`, `check-lock`, `fingerprint`, `diff` |
| [Deployment & Targets](#deployment--targets) | `deploy` |
| [AI Testing](#ai-testing) | `test`, `check-output` |
| [Library Management](#library-management) | `list`, `fmt`, `graph`, `impact`, `todos`, `stale` |
| [Pack System](#pack-system) | `pack init`, `pack build`, `pack install`, `pack list`, `pack remove`, `install`, `publish` |
| [Integrations](#integrations) | `mcp manifest`, `import`, `completion`, `lsp` |
| [Context & Summarisation](#context--summarisation) | `summarize` |
| [Recipes & Templates](#recipes--templates) | `recipe list`, `recipe apply` |
| [Journal](#journal) | `journal add`, `journal list` |
| [Interactive Tools](#interactive-tools) | `playground` |
| [Custom Commands](#custom-commands) | `execute` |

---

## Project Setup

---

### `loom init`

**What it does**

Creates the standard PromptLoom workspace in the current directory. Writes a `loom.toml` configuration file and creates the expected directory structure: `prompts/`, `blocks/`, `overlays/`, and `dist/`.

**Why it exists**

Before you can run any other `loom` command, the project needs a `loom.toml`. `loom init` gives you a working, annotated config in seconds so you can start writing prompts immediately.

**When to use it**

Run this once at the start of every new project, the same way you'd run `git init` or `npm init`.

**Syntax**

```
loom init [--sample]
```

**Flags**

| Flag | Description |
|---|---|
| `--sample` | Also write sample `BaseEngineer.prompt.loom` and `EngineeringDefaults.block.loom` files so you can see a working example immediately |

**Examples**

```bash
# Minimal init — just the config and empty dirs
loom init

# Init with sample prompts so you can run loom weave right away
loom init --sample
```

**What gets created**

```
loom.toml           ← project configuration
prompts/            ← your prompt source files
blocks/             ← your reusable block files
overlays/           ← optional overlay files
dist/               ← rendered Markdown output (auto-managed)
```

---

### `loom start`

**What it does**

Reads your `CLAUDE.md` (required) and `TODO.md` (optional) and generates a tailored starter prompt library for your specific project using an LLM. Detects your tech stack automatically or accepts an explicit `--stack` override. Presents a plan for you to review, edit, or cancel before writing any files.

**Why it exists**

Starting a prompt library from scratch on a real project is slow. `loom start` analyzes your project context and generates a meaningful set of prompts and blocks specifically for your tech stack, saving you from having to write the first five prompts by hand.

**When to use it**

After `loom init`, when you want a real prompt library to start from rather than empty directories. Especially useful on existing codebases where `CLAUDE.md` already captures the project's conventions.

**Syntax**

```
loom start [--nollm] [--minimal] [--best] [--stack <id>]
```

**Flags**

| Flag | Description |
|---|---|
| `--nollm` | Skip the LLM call entirely and use built-in templates for the detected stack. Faster and works without an API key. |
| `--minimal` | Generate a small library (3–5 prompts). Lower token cost. |
| `--best` | Generate the most comprehensive library possible. Higher token cost, highest quality. |
| `--stack <id>` | Override stack auto-detection. Accepted values: `go`, `python`, `typescript`, `javascript`, `rust`, `java`, `java-spring` |

**Examples**

```bash
# Auto-detect stack, call LLM, review the plan, then generate
loom start

# Use built-in Go templates — no API key needed
loom start --nollm --stack go

# Full quality library for a Java Spring Boot project
loom start --best --stack java-spring
```

**Requirements**

- `loom.toml` must exist (run `loom init` first)
- `CLAUDE.md` must exist in the project root

**How it works**

1. Scans your workspace for `CLAUDE.md`, `TODO.md`, and `go.mod` / `package.json` / etc. to detect the stack.
2. Calls the configured LLM (or uses built-in templates with `--nollm`) to generate a prompt plan.
3. Shows you the plan in a table. You can accept (`y`), open in your `$EDITOR` to modify (`e`), or cancel (`n`).
4. Writes only the files you approved.

---

### `loom thread`

**What it does**

Scaffolds a new `.loom` source file with the correct boilerplate for its type. The subcommands are:

- `loom thread prompt <Name>` — creates `prompts/<Name>.prompt.loom`
- `loom thread block <Name>` — creates `blocks/<Name>.block.loom`
- `loom thread overlay <Name>` — creates `overlays/<Name>.overlay.loom`
- `loom thread vars <Name>` — creates a `.vars.loom` file

**Why it exists**

Getting the file name, extension, and boilerplate structure exactly right is easy to get wrong. `loom thread` removes that friction.

**When to use it**

Every time you want to add a new prompt, block, or overlay to your library.

**Syntax**

```
loom thread prompt  <Name> [--inherits <ParentName>]
loom thread block   <Name>
loom thread overlay <Name>
loom thread vars    <Name>
```

**Flags (prompt only)**

| Flag | Description |
|---|---|
| `--inherits <Name>` | Pre-fill the `inherits` declaration in the generated prompt file |

**Examples**

```bash
# Create a new base prompt
loom thread prompt SecurityReviewer

# Create a child prompt pre-wired to inherit from CodeReviewer
loom thread prompt JavaReviewer --inherits CodeReviewer

# Create a reusable block
loom thread block SpringBootRules

# Create a terse overlay
loom thread overlay terse
```

---

## Rendering

---

### `loom weave`

**What it does**

The core rendering command. Resolves all inheritance, applies blocks, evaluates `from()` expressions, substitutes variables, and writes the final compiled prompt to `dist/` as a Markdown file (or another format you specify).

**Why it exists**

Your `.loom` source files are structured DSL, not the flat text an LLM receives. `loom weave` is the compiler step that turns source into output. It's the command you run most often.

**When to use it**

Whenever you want to see or use the final rendered output of a prompt. Run it after editing source files, or automatically in CI.

**Syntax**

```
loom weave [PromptName] [flags]
loom weave --all [flags]
```

**Flags**

| Flag | Description |
|---|---|
| `--all` | Render every prompt in the library |
| `--out <path>` | Write output to a specific file path (single prompt only) |
| `--stdout` | Print the rendered result to stdout instead of writing a file |
| `--format <fmt>` | Output format: `markdown` (default), `json-anthropic`, `json-openai`, `cursor-rule`, `copilot`, `claude-code`, `plain` |
| `--set <key=value>` | Set a `var` or `slot` value. Repeatable. |
| `--slot <key=value>` | Alias for `--set` |
| `--vars <file.toml>` | Load all variable values from a TOML file |
| `--profile <name>` | Load a named variable profile from `loom.toml` |
| `--variant <name>` | Apply a named `variant` block declared in the prompt |
| `--overlay <name>` | Apply an overlay. Repeatable for multiple overlays. |
| `--env <name>` | Apply a named `env` block (e.g. `prod`, `staging`) |
| `--with <spec>` | Attach live context to the prompt. Specs: `file:path`, `dir:path`, `git:diff`, `git:staged`, `stdin` |
| `--context <name>` | Load a pre-saved context bundle from `contexts/<name>.context` |
| `--sourcemap` | Write a `.loom.map.json` source-map alongside the rendered file (maps each field back to its source node) |
| `--watch` | Re-render automatically whenever a source file changes. Requires `--all`. |
| `--incremental` | Skip prompts whose resolved hash hasn't changed since the last render. Requires `--all`. Speeds up large libraries. |
| `--interactive` | Launch the guided prompt assembly wizard in the TUI |
| `--from <dir>` | Weave every prompt found in an arbitrary source directory (for example a pack's `source/` folder) instead of the current project. Only `--format`, `--variant`, `--env` and `--stdout` apply in this mode; variables, overlays and context are not applied |
| `--to <dir>` | With `--from`: the output directory. Defaults to `<from>/../compiled/` |

**Output formats**

| Format | Produces | Use for |
|---|---|---|
| `markdown` | `dist/<Name>.md` | General LLM paste, documentation |
| `json-anthropic` | JSON with `system` + `messages` structure | Anthropic API calls |
| `json-openai` | JSON with `messages` structure | OpenAI API calls |
| `cursor-rule` | `.cursorrules` format | Cursor editor rules |
| `copilot` | `.github/copilot-instructions.md` format | GitHub Copilot |
| `claude-code` | `.claude/commands/` format | Claude Code slash commands |
| `plain` | Raw field text, no headers | Piping into other tools |

**Examples**

```bash
# Render one prompt to dist/CodeReviewer.md
loom weave CodeReviewer

# Render all prompts
loom weave --all

# Print a prompt to stdout (great for piping)
loom weave CodeReviewer --stdout

# Supply a required slot value
loom weave BaseEngineer --set repo_name=my-api

# Apply a variant
loom weave GoCodeReviewer --variant strict

# Apply an env block for production
loom weave DataPipelineEngineer --env prod

# Apply an overlay at render time
loom weave GoCodeReviewer --overlay terse

# Render in Anthropic JSON format
loom weave CodeReviewer --format json-anthropic --stdout

# Attach the current git diff as context
loom weave CodeReviewer --with git:diff --stdout

# Watch mode — re-renders on every save
loom weave --all --watch

# Incremental — only re-render changed prompts
loom weave --all --incremental
```

---

### `loom cast`

**What it does**

Renders a prompt exactly like `loom weave` but sends the output to a **destination** instead of writing it to `dist/`. Supported destinations: clipboard, stdout, or a specific file path.

**Why it exists**

`loom weave` is for building the library. `loom cast` is for using a prompt right now — you cast it to your clipboard, paste it into an LLM chat, and you're done without touching the file system.

**When to use it**

When you want to use a prompt immediately — paste into Claude, ChatGPT, Cursor, or another tool without caring about the `dist/` output.

**Syntax**

```
loom cast <PromptName> [flags]
```

**Flags**

| Flag | Description |
|---|---|
| `--to <dest>` | Destination: `clipboard` (default), `stdout`, `file` |
| `--format <fmt>` | Same format options as `loom weave` |
| `--set <key=value>` | Set a variable. Repeatable. |
| `--slot <key=value>` | Alias for `--set` |
| `--vars <file.toml>` | Load variables from a TOML file |
| `--profile <name>` | Load a named variable profile |
| `--variant <name>` | Apply a named variant |
| `--overlay <name>` | Apply an overlay. Repeatable. |
| `--with <spec>` | Attach live context (`file:path`, `dir:path`, `git:diff`, `git:staged`, `stdin`) |
| `--context <name>` | Load a named context bundle |

**Examples**

```bash
# Render and copy to clipboard (default)
loom cast CodeReviewer

# Render and print to stdout
loom cast CodeReviewer --to stdout

# Cast with the git diff attached as context
loom cast CodeReviewer --with git:diff

# Cast in OpenAI JSON format
loom cast SecurityReviewer --format json-openai --to stdout

# Cast with staged files as context, paste-ready
loom cast BugFixer --with git:staged --to clipboard
```

---

### `loom copy`

**What it does**

A focused shortcut for `loom cast --to clipboard`. Renders a prompt and puts it on the clipboard in one command, with the same flags as `cast`.

**Why it exists**

The most common `cast` usage is copying to clipboard. `loom copy` makes it a first-class verb so the intent is immediately readable in scripts and shell history.

**When to use it**

When you want to copy a rendered prompt to your clipboard as fast as possible.

**Syntax**

```
loom copy <PromptName> [flags]
```

**Flags** — the same as `loom cast`, except that the destination is always the clipboard (there is no `--to`):

| Flag | Description |
|---|---|
| `--format <fmt>` | Same format options as `loom weave` |
| `--set <key=value>` | Set a variable. Repeatable. |
| `--slot <key=value>` | Alias for `--set` |
| `--vars <file.toml>` | Load variables from a TOML file |
| `--profile <name>` | Load a named variable profile from `loom.toml` |
| `--variant <name>` | Apply a named variant |
| `--overlay <name>` | Apply an overlay. Repeatable. |
| `--with <spec>` | Attach live context (`file:path`, `dir:path`, `git:diff`, `git:staged`, `stdin`) |
| `--context <name>` | Load a named context bundle from `contexts/<name>.context` |

**Examples**

```bash
loom copy CodeReviewer
loom copy GoCodeReviewer --variant strict
loom copy BaseEngineer --set repo_name=billing-service
```

---

## Validation & Inspection

---

### `loom inspect`

**What it does**

Validates every `.loom` source file in your project and reports errors and warnings. Checks include: undefined parent/block references, inheritance cycles, duplicate names, unknown field names, `from()` type errors, out-of-bounds parent indices, unresolved template tokens, and deprecated operator usage.

**Why it exists**

Catching mistakes at the source level (before rendering) is far cheaper than debugging a rendered prompt that silently dropped a field or inherited the wrong value. `loom inspect` is the type checker for your prompt library.

**When to use it**

- Before `loom weave` when you've made structural changes
- As the first step in any CI pipeline
- After adding a new inheritance relationship or cross-pack reference

**Syntax**

```
loom inspect
```

No flags. Always inspects the whole project.

**Exit codes**

| Code | Meaning |
|---|---|
| `0` | All clear — no errors, possibly warnings |
| `1` | One or more validation errors |
| `2` | Parse error (malformed DSL) |

**Example output**

```
  Inspecting 8 prompts and 3 blocks…

  ✗  GoCodeReviewer.prompt.loom:12  undefined block: GoConventions
  ✗  FullStackReviewer.prompt.loom:3  FrontendReviewer: parent not found
  ⚠  DataPipeline.prompt.loom:19  inheritance depth is 6 (max recommended: 5)

  Prompts: 8  Blocks: 3  Errors: 2  Warnings: 1
```

---

### `loom trace`

**What it does**

Shows the full resolution story for a prompt: its inheritance chain, which blocks are applied (and in what order), and exactly where each field's final value came from. Every field is annotated with the prompt or block that was the last to set it.

**Why it exists**

When a rendered prompt has unexpected content, you need to know *why*. `trace` answers "which ancestor set this field?" without you having to read every source file manually.

**When to use it**

When a rendered prompt doesn't match your expectations — trace it before looking at individual source files.

**Syntax**

```
loom trace [PromptName] [flags]
```

**Flags**

| Flag | Description |
|---|---|
| `--field <name>` | Show the complete resolution history for a single field only |
| `--instruction <text>` | Find which source node contributed the exact instruction that contains `<text>` |
| `--tree` | Show the inheritance and block dependency tree as a diagram |

**Examples**

```bash
# Full resolution trace for a prompt
loom trace SpringBootReviewer

# Show where the 'constraints' field came from at each level
loom trace SpringBootReviewer --field constraints

# Find the source of a specific instruction
loom trace GoCodeReviewer --instruction "context propagation"

# View the inheritance tree
loom trace SpringBootReviewer --tree
```

---

### `loom unravel`

**What it does**

Prints the fully resolved field values to stdout in plain text — all inheritance resolved, all `from()` expressions evaluated, all variables substituted — but without the Markdown headers that `loom weave` adds. Useful for inspecting the raw resolved content before it gets wrapped in a formatted document.

**Why it exists**

`loom weave --stdout` gives you the final Markdown. `unravel` gives you the raw field content before formatting — easier to diff, grep, or pipe into other tools.

**When to use it**

When you want to see exactly what each field contains after full resolution, without the rendered document structure.

**Syntax**

```
loom unravel <PromptName> [--with-source]
```

**Flags**

| Flag | Description |
|---|---|
| `--with-source` | Append a right-aligned source annotation to each field showing which node last set it |

**Examples**

```bash
loom unravel CodeReviewer
loom unravel SpringBootReviewer --with-source
```

---

### `loom contract`

**What it does**

Prints the `contract` and `capabilities` blocks declared in a prompt. No resolution — shows only what was declared in the source file.

**Why it exists**

When writing tests or checking output assertions, you need to quickly see the contract without opening the source file. `loom contract` gives you that view cleanly.

**When to use it**

Before writing test fixtures or when debugging `loom check-output` failures.

**Syntax**

```
loom contract <PromptName>
```

**Example output**

```
  Contract — SpringBootReviewer
  ────────────────────────────────────
  required_sections:
    - Issues Found
    - Verdict

  must_not_include:
    - "LGTM"
    - "looks good to me"

  Capabilities
  ────────────────────────────────────
  allowed:   read_code, suggest_changes
  forbidden: modify_production_code
```

---

## Code Quality

---

### `loom doctor`

**What it does**

Runs a comprehensive health check on one prompt or the entire library. Combines structural validation (like `inspect`) with smell detection and freshness checks. Produces a health score from 0–100 for each prompt, with a breakdown of what's dragging the score down.

**Why it exists**

`loom inspect` catches hard errors. `doctor` catches soft problems: prompts that are technically valid but have quality issues — fields that are too long, instruction lists that are too short, missing contracts, or dependencies that reference stale versions.

**When to use it**

On a schedule (daily CI, pre-release) to track library health over time. Also useful when adopting an existing prompt library you didn't write.

**Syntax**

```
loom doctor [PromptName] [--system]
```

Pass a name to check one prompt, omit it to check all.

| Flag | Description |
|---|---|
| `--system` | Check the **installation** instead of prompts: the loom version, whether the project loads, and the optional pieces some commands rely on (see below) |

**Installation check (`--system`)**

```
  ✓  loom           v5.0.0
  ✓  project        6 prompts, 3 blocks, 0 overlays (current directory)
  ✓  git            /usr/bin/git
  ⚠  model API key  $GEMINI_API_KEY is not set
                    only `loom test`, `loom summarize` and `loom start` call a model; put the key in .loomsecret or export it
  ⚠  registry       not configured
  ⚠  loomlocker     not found on PATH
```

Missing optional pieces are warnings that name the commands that need them, and do not change the exit code. Only a project that fails to load (or a malformed `loom.toml`) is a failure (exit 1). The key checked follows `[testing] provider` in `loom.toml` (`GEMINI_API_KEY` for Gemini, `ANTHROPIC_API_KEY` for Anthropic, or your `api_key_env`).

**Example output**

```
  SpringBootReviewer  health: 82/100
  ──────────────────────────────────────
  ⚠  persona field is very long (340 tokens) — consider trimming
  ⚠  no contract declared
  ⚠  instructions has 12 items — consider splitting into a block

  BaseEngineer  health: 96/100
  ──────────────────────────────────────
  ✓  All checks passed
```

---

### `loom smells`

**What it does**

A standalone smell report. Reports heuristic quality issues: vague instructions, overlong field values, instruction items that are contradictory, missing required fields, and unusual structural patterns.

**Why it exists**

`doctor` includes smells in its health score. `smells` is the command to reach for when you want just the smell analysis, without the full health-check overhead.

**When to use it**

During authoring, as a quick sanity check on a prompt you just wrote.

**Syntax**

```
loom smells [PromptName]
```

---

### `loom stats`

**What it does**

Shows a per-field token count estimate for one prompt (or all prompts sorted by total size). Helps you understand how large your rendered prompts are and which fields are consuming the most tokens.

**Why it exists**

Token length directly affects cost and model context limits. `stats` makes the token footprint of your prompts visible before you run them against an API.

**When to use it**

Before deploying a prompt in production. When optimising for cost. When a prompt is hitting context window limits.

**Syntax**

```
loom stats [PromptName] [--all] [--limit <N>]
```

**Flags**

| Flag | Description |
|---|---|
| `--all` | Show stats for all prompts, sorted by total token count (largest first) |
| `--limit <N>` | Warn when a prompt's total token count exceeds N |

**Examples**

```bash
# Token breakdown for one prompt
loom stats SpringBootReviewer

# All prompts, sorted by size
loom stats --all

# Flag any prompt over 800 tokens
loom stats --all --limit 800
```

---

### `loom minimize`

**What it does**

Scans your rendered prompts for exact duplicates and near-duplicates across field items (e.g. the same constraint appearing in two different prompts with slight wording differences). Reports findings and, with `--apply`, removes duplicates from the rendered output without touching your source files.

**Why it exists**

When a library evolves over months, duplicate rules quietly accumulate. `minimize` surfaces redundancy that's invisible when looking at individual files and makes the library leaner.

**When to use it**

As an occasional maintenance command — monthly, before a major release, or when the library has grown significantly.

**Syntax**

```
loom minimize [--apply] [--threshold <0-1>]
```

**Flags**

| Flag | Description |
|---|---|
| `--apply` | Remove duplicates from the rendered output in `dist/`. Source `.loom` files are not modified. |
| `--threshold <float>` | Similarity threshold for near-duplicate detection. Default: `0.85`. Lower = more aggressive matching. |

**Examples**

```bash
# Read-only report — what's duplicated?
loom minimize

# Remove exact duplicates from rendered output
loom minimize --apply

# More aggressive near-duplicate matching
loom minimize --threshold 0.75 --apply
```

---

### `loom audit`

**What it does**

Scans the *resolved* text of prompts for instructions that are risky to ship: references to hardcoded secrets, safety or policy bypasses, destructive commands with no confirmation step, direct production-environment references, PII fields with no privacy qualifier, instructions that remove a user confirmation gate, and urgency instructions with no safety qualifier. Each finding names the field, the offending text, why it is flagged, and a suggested fix.

Telling the model *not* to do something risky is not a finding: "never skip the tests" passes, "skip the tests" does not.

**Why it exists**

Prompts run with real permissions (agents, MCP tools, deploy assistants). A line like "ignore the confirmation prompt" is a security bug, and it is easy to inherit from a parent prompt or block without noticing. `audit` checks the final, inherited result rather than each file in isolation.

**When to use it**

Before deploying prompts that drive tools or agents, and in CI alongside `loom inspect` and `loom doctor`.

**Syntax**

```
loom audit [Name] [--all]
```

With no name, every prompt is audited.

**Flags**

| Flag | Description |
|---|---|
| `--all` | Audit every prompt (the default when no name is given) |

**Exit codes**

| Code | Meaning |
|---|---|
| `0` | No findings |
| `1` | At least one high-risk finding |
| `2` | Medium-risk findings only |

**Examples**

```bash
loom audit
loom audit DeployAssistant
```

## Git & History

---

### `loom blame`

**What it does**

Traces every item in every resolved field of a prompt back to the git commit that last changed it. Shows commit hash, author, and date alongside each field item.

**Why it exists**

Prompts evolve through many commits. When something in a prompt is wrong or unexpected, blame answers "who added this and when?" — the same question `git blame` answers for source code.

**When to use it**

When investigating why a specific rule or instruction is in a prompt, or when auditing recent changes.

**Syntax**

```
loom blame <Name> [--field <name>] [--since <date|ref>] [--instruction <text>]
```

**Flags**

| Flag | Description |
|---|---|
| `--field <name>` | Limit output to one field only (e.g. `constraints`) |
| `--since <date\|ref>` | Only show items changed after this date (`YYYY-MM-DD`) or git ref (`HEAD~10`) |
| `--instruction <text>` | Filter to items containing this text |

**Examples**

```bash
# Full blame for a prompt
loom blame SpringBootReviewer

# Who changed the constraints recently?
loom blame SpringBootReviewer --field constraints --since 2026-01-01

# Find who added the "no debug logging" rule
loom blame DataPipeline --instruction "debug logging"
```

---

### `loom changelog`

**What it does**

Scans git history for changes to `.loom` source files and presents a prompt-centric change log. Organises commits by which prompt or block was modified and what changed at the field level.

**Why it exists**

Git log is commit-centric. `changelog` is prompt-centric: it answers "what changed in `CodeReviewer` over the last month?" rather than requiring you to scan commits and cross-reference file paths.

**When to use it**

Before a release, when writing a handover, or when reviewing what changed in a prompt library sprint.

**Syntax**

```
loom changelog [Name] [--since <date|ref>] [--format text|markdown]
```

**Flags**

| Flag | Description |
|---|---|
| `--since <date\|ref>` | Limit history to commits after this date or ref |
| `--format text\|markdown` | Output format. Default: `text`. Use `markdown` to paste into a PR description. |

**Examples**

```bash
# All changes in the last 30 days
loom changelog --since 2026-04-22

# Changes to one prompt since a tag
loom changelog SpringBootReviewer --since v1.2.0

# Markdown output for a PR description
loom changelog --since HEAD~10 --format markdown
```

---

### `loom diff`

**What it does**

Shows a field-aware diff between two prompts or between the current resolved state of a prompt and its last-rendered file in `dist/`. Unlike a line diff, it understands prompt structure and highlights which fields changed and how.

**Why it exists**

A regular `git diff` on `.loom` files shows you what changed in source syntax. `loom diff` shows you what the rendered prompt actually looks like before and after — the diff that matters for the model receiving it.

**When to use it**

Before committing prompt changes. In CI to detect renders that are out of date with their source.

**Syntax**

```
loom diff [PromptA] [PromptB]
loom diff [PromptName] --against-dist
loom diff --all --against-dist
```

**Flags**

| Flag | Description |
|---|---|
| `--against-dist` | Compare the current resolved prompt against its last-rendered `dist/` file |
| `--all` | Diff all prompts. Requires `--against-dist`. |
| `--exit-code` | Exit 1 if any changes are found. Useful in CI. |
| `--semantic` | Show semantic change classifications (added / removed / modified / reordered) instead of a raw line diff |

**Examples**

```bash
# Diff two prompts side by side
loom diff BaseEngineer CodeReviewer

# Is the dist file stale?
loom diff CodeReviewer --against-dist

# CI gate — fail if any rendered output is out of date
loom diff --all --against-dist --exit-code

# Semantic change summary instead of line diff
loom diff CodeReviewer --against-dist --semantic
```

---

### `loom review`

**What it does**

Aggregates all prompt diffs into a single structured Markdown PR review summary. Compares current rendered prompts against either `dist/` or a git ref, and produces a document suitable for pasting into a pull request description.

**Why it exists**

Reviewers on a prompt-heavy PR need to see what actually changed in the model-facing output, not just the DSL source. `loom review` generates that summary automatically.

**When to use it**

Before opening a pull request that touches prompts. Can also be run in CI to post the summary as a PR comment.

**Syntax**

```
loom review [--since <ref>]
```

**Flags**

| Flag | Description |
|---|---|
| `--since <ref>` | Compare against a specific git ref (e.g. `HEAD~3`, `main`, `v1.2.0`) |

**Examples**

```bash
# Review all changes against dist/
loom review

# Review changes since a specific commit
loom review --since HEAD~3

# Review changes since main
loom review --since main
```

---

## CI & Locking

---

### `loom ci`

**What it does**

A single command that runs all CI gates in sequence:

1. `loom inspect` — syntax, ref, and cycle validation
2. `loom doctor` — health scores and smell detection
3. `loom check-lock` — verify `dist/` fingerprints match `loom.lock`
4. `loom diff --all --against-dist` — confirm rendered output is not stale
5. `loom deploy --check` — when `[[targets]]` are configured, confirm the deployed files (`CLAUDE.md`, `AGENTS.md`, …) are in sync

Exits 0 only if every gate passes. Exits 1 on the first failure with a clear message identifying which gate failed and why.

**Why it exists**

Prompt library CI should be one command. `loom ci` replaces four separate commands with a single composable gate that you add to your GitHub Actions / GitLab CI / whatever pipeline.

**When to use it**

In your CI pipeline on every pull request that touches `.loom` files. Configure it as a required status check.

**Syntax**

```
loom ci
```

No flags.

**Example CI YAML**

```yaml
- name: Validate prompt library
  run: loom ci
```

**Exit codes**

| Code | Meaning |
|---|---|
| `0` | All gates pass |
| `1` | One or more gates failed (message identifies which) |

---

### `loom lock`

**What it does**

Computes a stable fingerprint (SHA-256 hash of the fully resolved content) for every prompt and block in the library and writes those fingerprints to `loom.lock`. Think of it as `go.sum` for your prompt library.

**Why it exists**

Without a lockfile, there is no reliable way to detect that a rendered prompt is out of date or that something changed unexpectedly. `loom.lock` is the source of truth for what the library should produce.

**When to use it**

After any intentional change to prompts or blocks. Commit `loom.lock` alongside your source changes so CI can verify consistency.

**Syntax**

```
loom lock
```

No flags.

---

### `loom check-lock`

**What it does**

Verifies that the current resolved fingerprints of all prompts and blocks match the values recorded in `loom.lock`. Exits 1 if any fingerprint has drifted — meaning a source file changed but `loom lock` was not re-run, or a rendered `dist/` file was hand-edited.

**Why it exists**

The lockfile is only useful if you actually check it. `check-lock` is the enforcement step — it's what `loom ci` runs to guarantee the library is in a consistent, committed state.

**When to use it**

In CI. Run after `loom inspect` but before deploying. Automatically included in `loom ci`.

**Syntax**

```
loom check-lock
```

No flags. Exits 0 if all fingerprints match, 1 on any mismatch.

---

### `loom fingerprint`

**What it does**

Prints the stable fingerprint for one resolved prompt. The fingerprint is a deterministic hash of the prompt's fully resolved field content — the same value stored in `loom.lock`.

**Why it exists**

Useful for scripting — you can detect whether a prompt has changed between builds by comparing fingerprints without running the full validation stack.

**When to use it**

In scripts or debugging to check whether a specific prompt's output has changed.

**Syntax**

```
loom fingerprint <PromptName>
```

---

## Deployment & Targets

---

### `loom deploy`

**What it does**

Reads the `[[targets]]` table in `loom.toml` and renders each configured prompt to its target path in the appropriate format. Supports Claude Code commands, Copilot instructions, Cursor rules, AGENTS.md, and plain Markdown files. Lets you keep every AI tool in your workflow fed from a single source of truth.

**Why it exists**

Most teams use more than one AI tool. Writing the same instructions separately for Claude Code, Copilot, and Cursor is error-prone and quickly diverges. `loom deploy` makes your prompt library the single source and pushes to all targets.

**When to use it**

After any prompt change that should be reflected in your AI tool configuration files. Can be automated in a post-commit hook or CI step.

**Syntax**

```
loom deploy [--dry-run] [--diff] [--check] [--target <format>]
```

**Flags**

| Flag | Description |
|---|---|
| `--dry-run` | Preview which target files would be written without touching the filesystem |
| `--diff` | Show a line diff for any target file that would change |
| `--check` | Write nothing; exit 1 if any target file is missing or differs from what its prompt renders to. Catches a hand-edited `CLAUDE.md` / `AGENTS.md` / Cursor rule that drifted from its prompt, and a prompt that changed without a redeploy. `loom ci` runs it automatically when targets are configured |
| `--target <format>` | Only deploy targets of a specific format (e.g. `claude-code`, `copilot`, `cursor-rule`) |

**`loom.toml` target configuration**

```toml
[[targets]]
prompt  = "CodeReviewer"
format  = "claude-code"
dest    = ".claude/commands/review.md"

[[targets]]
prompt  = "BaseEngineer"
format  = "copilot"
dest    = ".github/copilot-instructions.md"

[[targets]]
prompt  = "CodeReviewer"
format  = "cursor-rule"
dest    = ".cursor/rules/review.mdc"

[[targets]]
prompt  = "BaseEngineer"
format  = "markdown"
dest    = "AGENTS.md"
```

Each target has exactly three keys: `prompt` (the prompt to render), `format` (`markdown`, `claude-code`, `copilot`, `cursor-rule`, `json-anthropic`, `json-openai` or `plain`) and `dest` (the file to write, relative to the project). A prompt with required `slot`s that have no value cannot be deployed: that target fails with the unresolved variables listed.

**Examples**

```bash
# Deploy all configured targets
loom deploy

# Preview what would be written
loom deploy --dry-run

# CI: fail if the deployed files are out of date
loom deploy --check

# Show what changed in each target file
loom deploy --diff

# Only deploy Claude Code targets
loom deploy --target claude-code
```

---

## AI Testing

---

### `loom test`

**What it does**

Sends a resolved prompt to a real AI model (Gemini or Anthropic) with a test fixture and asserts the response against the prompt's declared `contract` block. Supports recording a baseline response and comparing future runs against it.

**Why it exists**

Structural validation (`loom inspect`) confirms your DSL is correct. `loom test` confirms the prompt actually produces correct output when given to a real model. It's the integration test for your prompt library.

**When to use it**

After making changes to a prompt's core instructions or contract. As a nightly CI job. When adopting a model upgrade and verifying prompts still behave correctly.

**Syntax**

```
loom test [Name] [--all] [--model <id>] [--record] [--compare]
```

**Flags**

| Flag | Description |
|---|---|
| `--all` | Test every prompt in the library |
| `--model <id>` | Override the model (e.g. `gemini-2.0-flash`, `claude-sonnet-4-6`) |
| `--record` | Record the current model response as the golden baseline |
| `--compare` | Compare the current response against the recorded baseline |

**Requirements**

- API key set via the environment variable in `loom.toml` (default: `$GEMINI_API_KEY` for Gemini, `$ANTHROPIC_API_KEY` for Anthropic)
- Prompt must have a `contract` block to assert against

**Examples**

```bash
# Test one prompt
loom test SpringBootReviewer

# Test all prompts
loom test --all

# Test against a specific model
loom test SecurityReviewer --model claude-sonnet-4-6

# Record a golden baseline
loom test CodeReviewer --record

# Compare against baseline (detect regressions)
loom test CodeReviewer --compare
```

---

### `loom check-output`

**What it does**

Reads an existing output file (a model response you already have) and validates it against a prompt's `contract` block. Does not call any model — it checks a file you provide.

**Why it exists**

Sometimes you have a model response you captured elsewhere — from a log, a screenshot, or a manual run — and want to verify it against the contract without running the model again. `check-output` does that offline check.

**When to use it**

When debugging a suspected contract violation. When validating responses captured from production logs.

**Syntax**

```
loom check-output <PromptName> <output-file>
```

**Examples**

```bash
# Validate a captured response file
loom check-output SpringBootReviewer response.txt

# Validate from stdin
cat response.txt | loom check-output SpringBootReviewer -
```

---

## Library Management

---

### `loom list`

**What it does**

Lists all prompts and blocks in the current project, their inheritance parents, the blocks they use, and a one-line summary (from the `summary` field if declared).

**Why it exists**

As a library grows past a handful of files, you need a quick index. `loom list` is the card catalogue for your prompt library.

**When to use it**

When you join a project with an existing library. When you can't remember the name of a prompt. When onboarding teammates.

**Syntax**

```
loom list [--prompts] [--blocks]
```

**Flags**

| Flag | Description |
|---|---|
| `--prompts` | List only prompts (omit blocks) |
| `--blocks` | List only blocks (omit prompts) |

---

### `loom fmt`

**What it does**

Reformats every `.loom` source file in the project to the canonical style: consistent indentation, correct spacing around operators, normalised `from()` expressions, and blank-line separation between fields. Idempotent — running it twice produces the same result as running it once.

**Why it exists**

Without a formatter, everyone on the team writes DSL slightly differently. `loom fmt` is the `gofmt` / `prettier` for PromptLoom — it enforces one style so diffs stay meaningful.

**Semantic simplifications it applies:**

- Removes empty `and {}` literal blocks (they are no-ops)
- Deduplicates adjacent identical `from()` units
- Collapses single-element ranges: `from(parent[0..1])` → `from(parent[0])`

**What it preserves**

Formatting never changes what a prompt means or drops anything you wrote:

- **Comments** (`//` lines) stay attached to the element that follows them, even when canonical ordering moves that element. A comment after the last element of a prompt stays at the end of its body, and file-level comments stay where they are. Blank lines you left between comments are kept.
- `env` blocks, `variant` blocks, `tags`, `use` lines, `contract` and `capabilities` entries.
- Variable and slot metadata: `secret: true`, `required: false`, `default: "…"`.

Body elements are written in a fixed order: `tags`, variables/slots, `use`, fields, `variant` blocks, `env` blocks, `contract`, `capabilities`.

Known limits: a comment written *inside* a list of items, or inside a `contract`/`capabilities`/`variant`/`env` body after its last entry, moves to just before the next element.

**Safety net**

After formatting, `loom fmt` parses its own output and compares an inventory of everything the file declares (fields, operators, variables and their metadata, variants, env blocks, contract entries, tags) and the number of comments with the original. If anything differs it reports the file, **leaves it untouched**, and exits non-zero.

**When to use it**

Before committing. In a pre-commit hook. In CI with `--check` to fail on unformatted files.

**Syntax**

```
loom fmt [--check] [--migrate]
```

**Flags**

| Flag | Description |
|---|---|
| `--check` | Report unformatted files without modifying them. Exits 1 if any files would change. |
| `--migrate` | Upgrade v1 syntax to v2 instead of formatting (see below). Combine with `--check` to preview. |

**Examples**

```bash
# Format all .loom files in place
loom fmt

# CI check — fail if anything is unformatted
loom fmt --check

# Upgrade a v1 project to v2 syntax
loom fmt --migrate
```

**Migrating v1 projects (`--migrate`)**

Since v2 the only field operator is `:=`. `loom fmt --migrate` rewrites everything that has one obvious v2 meaning:

| v1 | becomes |
|---|---|
| `prompt A extends B {` | `prompt A inherits B {` |
| `persona:` (bare colon) | `persona :=` |
| `instructions +=` in a prompt with a parent | `instructions := from(parent[*]) and { … }` |
| `constraints +=` in a block or overlay | `constraints :=` |
| `instructions +=` in a prompt with no parent | `instructions :=` |

Comments and layout of the rewritten files are kept, every result is parsed again before it is written, and a file that would lose anything is left untouched. Running it again changes nothing.

Anything that has no mechanical equivalent is **listed with its file and line and left exactly as written**, so `loom inspect` keeps reporting it until you decide:

- `-=` (v2 has no removal operator; write the list you want, or select parent items with `parent[0].field[1..3]`)
- `+=` on a scalar field that is inherited (v2 replaces scalars; choose the final text)
- `+=` in a prompt whose `use`d block also defines the same list — v1 added to the block's items, but in v2 a prompt that writes the list replaces them, so the choice is yours
- `+=` inside a `variant` or `env` block, and a field declared twice in one body

The command exits 1 while any of those remain (and, with `--check`, while any file still needs migrating), so it can gate a CI job during an upgrade.

---

### `loom graph`

**What it does**

Renders the dependency graph of the entire prompt library: inheritance relationships and block usage. With a prompt or block name it shows only that thing's **neighbourhood**: the prompts it inherits from (nearest first), the prompts that inherit from it, and the blocks it gets (marking which come through an ancestor). For a block: the prompts that use it and everything below them. Launches an interactive split-panel TUI browser by default. Can also output text, Mermaid diagram syntax, or Graphviz DOT for embedding in documentation.

**Why it exists**

When a library has 20+ prompts with complex inheritance and multiple blocks, it becomes impossible to hold the full dependency graph in your head. `loom graph` makes the structure visible.

**When to use it**

When refactoring inheritance hierarchies. When planning where to put a new block. When presenting the library architecture to your team.

**Syntax**

```
loom graph [PromptName] [--format ascii|mermaid|dot] [--unused] [--no-interactive]
```

**Flags**

| Flag | Description |
|---|---|
| `--format ascii\|mermaid\|dot` | Output format. Default: `ascii`. `mermaid` and `dot` produce markup you can paste into docs. |
| `--unused` | Highlight blocks that are defined but not used by any prompt |
| `--no-interactive` | Print plain text instead of launching the TUI browser. Useful for piping. |

**Examples**

```bash
# Interactive TUI graph browser (default)
loom graph

# Everything related to one prompt: what it inherits from, what inherits from it, its blocks
loom graph SpringBootReviewer --no-interactive

# The same neighbourhood as a diagram (only related prompts and blocks are drawn)
loom graph SpringBootReviewer --format mermaid

# Mermaid diagram for embedding in a README
loom graph --format mermaid --no-interactive

# Show unused blocks
loom graph --unused
```

---

### `loom impact`

**What it does**

Shows the **blast radius** of changing a prompt or a block: every prompt that would be affected, split into *direct* and *transitive* dependents.

- For a **prompt**: the prompts that inherit from it, and everything further down the inheritance chain.
- For a **block**: the prompts that use it, and everything that inherits from those (they resolve to a parent that includes the block).

**Why it exists**

Base prompts and shared blocks are the most leveraged files in a library: one edit changes many rendered prompts. `impact` answers "what breaks if I touch this?" before you do.

**When to use it**

Before refactoring a base prompt, tightening a shared block, or deleting something you think is unused ("Nothing depends on it: it is safe to change or remove"). With `--json` in CI to flag risky changes.

**Syntax**

```
loom impact <Name> [--json]
```

**Flags**

| Flag | Description |
|---|---|
| `--json` | Print `{"name", "kind", "direct", "transitive", "total"}` instead of text |

An unknown name fails with a *did you mean* suggestion.

**Examples**

```bash
loom impact BaseEngineer
loom impact SecurityChecklist --json
```

---

### `loom todos`

**What it does**

Lists every item in a `todo:` field across all prompts and blocks in the library, in one view.

**Why it exists**

`todo:` is a structured field for leaving notes about future work on a prompt. `loom todos` aggregates all of them so nothing slips through the cracks.

**When to use it**

During sprint planning. Before a library release, to make sure all TODOs have been addressed.

**Syntax**

```
loom todos
```

No flags.

---

### `loom stale`

**What it does**

Scans your dependency files (`go.mod`, `package.json`, `Cargo.toml`, `pom.xml`, `pyproject.toml`, `requirements.txt`) for pinned library versions, then scans your prompts for mentions of a different version of the same library. Reports mismatches as stale references.

**Why it exists**

Prompts often say things like "use Gin v1.9.0" or "the project runs on Spring Boot 3.2". When you upgrade a dependency, these version mentions rot silently. `loom stale` is the automated cross-check.

**When to use it**

After upgrading a major dependency. As a regular maintenance sweep. In CI after `package.json` or `go.mod` changes.

**Syntax**

```
loom stale
```

No flags. Always checks all prompts against all detected dependency files.

---

## Pack System

---

### `loom pack init`

**What it does**

Creates a `.metadata.loom`, `.dependency.loom`, and `.export.loom` scaffold in the current directory, turning it into a proper PromptLoom pack.

**When to use it**

When you want to publish a set of prompts as a distributable pack that others can install.

**Syntax**

```
loom pack init
```

---

### `loom pack build`

**What it does**

Bundles the current pack directory (its `prompts/`, `blocks/`, `overlays/`, and dot-files) into a `.lpack` archive file, ready for distribution or installation.

**When to use it**

When preparing a pack for publishing or for sharing with a teammate who will install it manually.

**Syntax**

```
loom pack build
```

---

### `loom pack install <path>`

**What it does**

Unpacks a `.lpack` archive file into the current project's pack directory, making its prompts and blocks available for cross-pack inheritance and block usage.

**When to use it**

When installing a pack you received as a file (not from the registry).

**Syntax**

```
loom pack install <path-to.lpack>
```

---

### `loom pack list`

**What it does**

Lists all packs currently installed in the project, with their slug, version, and source.

**Syntax**

```
loom pack list
```

---

### `loom pack remove <name>`

**What it does**

Removes an installed pack by slug. Prompts that reference the removed pack will fail `loom inspect` after removal.

**Syntax**

```
loom pack remove <slug>
```

---

### `loom install`

**What it does**

Downloads and installs a named pack from the PromptLoom registry. Recursively resolves and installs all pack dependencies declared in `.dependency.loom`. Writes exact installed versions to `loompack.lock`.

**Why it exists**

Packs can depend on other packs. `loom install` handles the full dependency graph, detects version conflicts, and guarantees a reproducible installation.

**When to use it**

When adding a published pack to your project.

**Syntax**

```
loom install <vault-name> [--registry <url>]
```

**Flags**

| Flag | Description |
|---|---|
| `--registry <url>` | Registry base URL (overrides every other source) |

**Registry resolution.** There is **no built-in default registry**. The URL comes from, in order: `--registry`, `$LOOM_REGISTRY_URL`, `LOOM_REGISTRY_URL` in `loom/.loom.env`, then `[registry] url` in `loom.toml`. If none is set, the command explains how to configure one. `http://` is accepted for `localhost`; for any other host a warning is printed, since packs would be downloaded unencrypted.

**Dependencies.** A pack lists the packs it needs in `.dependency.loom` (one per line, e.g. `go-foundation>=1.0.0`; the file format is in [LOOM_LANGUAGE.md](LOOM_LANGUAGE.md#dependencyloom)). `loom install` installs the pack, then every dependency it declares, then theirs, and prints transitive packs with a `↳` marker. Each pack is installed once even if several packs depend on it, and a pack already installed at a version that satisfies every constraint is not downloaded again. The exact versions installed are recorded in `loompack.lock`.

If two packs require incompatible versions of the same dependency, every conflict is printed with the constraints that clash and who required each one, and the command exits with an error; nothing is resolved automatically, so edit the constraints (or the lock) and run it again.

**Safety.** Before writing anything, the installer rejects packs whose slug or file paths are unsafe (absolute paths, `..`, backslashes), so a malicious registry cannot write outside `loompack/<slug>/`.

**Examples**

```bash
loom install go-backend --registry https://registry.example.com
LOOM_REGISTRY_URL=http://localhost:8080 loom install go-backend
```

---

### `loom publish`

**What it does**

Uploads a pack directory to the PromptLoom registry. Reads metadata from `.metadata.loom` and bundles `prompts/`, `blocks/`, `overlays/`, and dependency files.

**When to use it**

When sharing a pack publicly or with your team via the registry.

**Syntax**

```
loom publish <pack-dir> [--registry <url>] [--secret <key>] [--dry-run]
```

**Flags**

| Flag | Description |
|---|---|
| `--registry <url>` | Target registry URL |
| `--secret <key>` | API key for authenticated publish |
| `--dry-run` | Validate and bundle without uploading |

**Examples**

```bash
# Publish the current directory as a pack
loom publish .

# Dry run first to check the bundle
loom publish . --dry-run

# Publish to a private registry
loom publish . --registry https://registry.my-company.com --secret $UPLOAD_SECRET
```

---

## Integrations

---

### `loom mcp manifest`

**What it does**

Generates an MCP (Model Context Protocol) tool manifest from one or all prompts. The manifest is a JSON file that describes your prompts as MCP-compatible tools, so any MCP-aware host can discover and invoke them.

**Why it exists**

MCP is an emerging standard for describing AI tools. `loom mcp manifest` lets your prompt library participate in the MCP ecosystem without manual JSON authoring.

**When to use it**

When integrating your prompt library with an MCP host (Claude Desktop, other MCP clients).

**Syntax**

```
loom mcp manifest [Name] [--all] [--out <file>]
```

**Flags**

| Flag | Description |
|---|---|
| `--all` | Generate a manifest for all prompts |
| `--out <file>` | Write the manifest JSON to a file instead of stdout |

**Examples**

```bash
# Manifest for one prompt
loom mcp manifest SpringBootReviewer

# Full library manifest, written to a file
loom mcp manifest --all --out mcp-manifest.json
```

---

### `loom import`

**What it does**

Converts an existing Markdown prompt file into PromptLoom DSL. Uses heuristic parsing to detect headings as field names, bullet lists as list fields, and paragraph text as scalar fields. Writes a `.prompt.loom` file to the prompts directory.

**Why it exists**

Most teams have existing prompts written as flat Markdown. `loom import` is the migration path — bring your existing library in without rewriting everything from scratch.

**When to use it**

When migrating an existing Markdown prompt library. When a teammate shares a prompt in Markdown format.

**Syntax**

```
loom import [file.md] [--name <Name>] [--out <dir>] [--dir <source-dir>] [--force]
```

**Flags**

| Flag | Description |
|---|---|
| `--name <Name>` | Override the prompt name (default: derived from filename) |
| `--out <dir>` | Output directory for the generated `.loom` file (default: `prompts/`) |
| `--dir <dir>` | Import all `.md` files from this directory at once |
| `--force` | Overwrite existing `.loom` files if they already exist |

**Examples**

```bash
# Import a single Markdown prompt
loom import docs/reviewer.md

# Import with a specific name
loom import docs/reviewer.md --name CodeReviewer

# Bulk import from a directory
loom import --dir docs/prompts/

# Overwrite existing files
loom import old-prompts/ --dir --force
```

---

### `loom completion`

**What it does**

Prints a shell completion script for bash, zsh, fish or PowerShell. Once installed, `Tab` completes commands and flags, and — because it reads the current project — **prompt and block names**, `--overlay` names, `--variant` / `--env` names of the prompt you already typed, and `--format` values.

```
loom weave Sec<Tab>          → SecurityReviewer   (prompt · inherits CodeReviewer)
loom impact Sec<Tab>         → SecurityChecklist  (block), SecurityReviewer
loom weave X --variant <Tab> → the variants declared on X
loom weave --format j<Tab>   → json-anthropic  json-openai
```

Outside a project, or when the library does not load, only commands and flags are offered.

**Syntax**

```
loom completion bash|zsh|fish|powershell
```

**Installing**

```bash
# bash (load for this shell / add to ~/.bashrc)
source <(loom completion bash)

# zsh
loom completion zsh > "${fpath[1]}/_loom"     # then restart the shell

# fish
loom completion fish > ~/.config/fish/completions/loom.fish

# PowerShell
loom completion powershell | Out-String | Invoke-Expression
```

---

### `loom lsp`

**What it does**

Starts the PromptLoom Language Server Protocol server on `stdin/stdout`. The server provides diagnostics, code completion, hover documentation, go-to-definition and a document outline for `.loom` files, for any editor with an LSP client (a Neovim setup is in [neovim-lsp.md](neovim-lsp.md)). The Lumine VS Code extension ships its own language server and does not use `loom lsp`.

**Why it exists**

The LSP protocol is the standard way for editors to communicate with language tooling. `loom lsp` is what editors other than VS Code connect to.

**When to use it**

You typically don't run this manually; your editor's LSP client starts it. Run it by hand only to debug LSP communication.

**Syntax**

```
loom lsp
```

No flags. Reads from stdin, writes to stdout (the LSP protocol).

---

## Context & Summarisation

---

### `loom summarize`

**What it does**

Uses an LLM to generate a structured Markdown summary of your project, a directory, or specific files. The summary describes architecture, key files, conventions, and notable patterns — useful for seeding `CLAUDE.md` or providing context for new team members.

**Why it exists**

`loom start` needs good project context to generate a useful prompt library. `loom summarize` creates that context by having an LLM read your codebase first.

**When to use it**

Before `loom start` on a large existing project. When writing or updating `CLAUDE.md`. When onboarding a new team member who needs a quick codebase overview.

**Syntax**

```
loom summarize workspace|<path...> [--save] [--out <file>]
```

**Flags**

| Flag | Description |
|---|---|
| `--save` | Save the generated summary to `.loom/context/` for use by `loom start` and context bundles |
| `--out <file>` | Write output to a specific file path |

**Examples**

```bash
# Summarise the whole project
loom summarize workspace

# Summarise a specific directory
loom summarize src/services/

# Summarise specific files
loom summarize main.go router.go

# Save for use in loom start
loom summarize workspace --save
```

---

## Recipes & Templates

---

### `loom recipe list`

**What it does**

Lists all built-in recipe templates available in `loom recipe apply`. Each recipe is a pre-built prompt library for a specific role, language, or framework.

**Syntax**

```
loom recipe list
```

---

### `loom recipe apply`

**What it does**

Scaffolds a prompt library from a built-in recipe template. Recipes are curated starting points for common engineering contexts: code reviewer, API designer, test writer, security auditor, and more.

**Why it exists**

`loom start` generates a library from your specific project. `recipe apply` gives you a high-quality opinionated template without needing a `CLAUDE.md` or API call. Good for getting started fast on greenfield projects.

**When to use it**

When starting a new project and you want a quality starting point now, without the analysis step of `loom start`.

**Syntax**

```
loom recipe apply <recipe> [--language <lang>] [--framework <fw>] [--style rest|graphql] [--force]
```

**Flags**

| Flag | Description |
|---|---|
| `--language <lang>` | Programming language for the recipe (e.g. `go`, `rust`, `java`, `typescript`) |
| `--framework <fw>` | Framework to target (e.g. `gin`, `axum`, `spring-boot`, `react`) |
| `--style rest\|graphql` | API style, for `api-designer` recipe |
| `--force` | Overwrite existing files |

**Examples**

```bash
# See what's available
loom recipe list

# Go code reviewer
loom recipe apply code-reviewer --language go

# Spring Boot API designer
loom recipe apply api-designer --language java --framework spring-boot

# TypeScript test writer
loom recipe apply test-writer --language typescript --framework jest
```

---

## Journal

---

### `loom journal add`

**What it does**

Creates a new journal entry in `.loom/journal/` with a timestamp, optional author, optional associated prompt name, and an optional Markdown body. The journal is a lightweight change log for decisions, experiments, and notes that are too nuanced for git commit messages.

**Why it exists**

Prompt design involves a lot of trial-and-error and reasoning that git history doesn't capture. The journal is the decision log — "why did we change this constraint?" or "we tried a shorter persona and it made responses worse."

**When to use it**

When making a significant change to a prompt. When experimenting with different instruction phrasings. When you want to record context that explains a `git blame` result.

**Syntax**

```
loom journal add <message> [--prompt <Name>] [--author <name>] [--body <text>]
```

**Flags**

| Flag | Description |
|---|---|
| `--prompt <Name>` | Associate this entry with a specific prompt |
| `--author <name>` | Author name (defaults to git config `user.name`) |
| `--body <text>` | Additional Markdown body text for longer notes |

**Examples**

```bash
# Simple note
loom journal add "Tightened persona language — previous version was too verbose"

# Linked to a specific prompt
loom journal add "Removed LGTM constraint — too aggressive" --prompt CodeReviewer

# With a detailed body
loom journal add "Switched to from(parent[*]) merge" \
  --prompt SpringBootReviewer \
  --body "The explicit list was getting out of sync with the parent. Merge is more maintainable."
```

---

### `loom journal list`

**What it does**

Lists all journal entries, newest first. Can be filtered to entries associated with a specific prompt.

**Syntax**

```
loom journal list [PromptName]
```

**Examples**

```bash
# All journal entries
loom journal list

# Only entries for one prompt
loom journal list CodeReviewer
```

---

## Interactive Tools

---

### `loom playground`

**What it does**

Opens an interactive TUI for a prompt where you can:

- Switch between variants in real time
- Toggle overlays on and off
- Change the output format
- See the rendered output update live as you adjust controls
- Export the current configuration as a `loom weave` command

**Why it exists**

Understanding how variants, overlays, and formats interact is hard to do by reading source files and re-running `loom weave` manually. The playground makes it visual and instant.

**When to use it**

When tuning a prompt's variants or overlays. When showing a prompt to a teammate and walking them through the options. When you're not sure which output format suits your use case.

**Syntax**

```
loom playground <Name>
```

No flags. The controls are inside the TUI.

**TUI controls**

| Key | Action |
|---|---|
| `v` | Cycle through available variants |
| `o` | Toggle an overlay |
| `f` | Switch output format |
| `e` | Export the current configuration as a CLI command |
| `q` | Quit |

**Example**

```bash
loom playground GoCodeReviewer
```

---

## Custom Commands

---

### `loom execute`

**What it does**

Looks up a named custom command in the `[custom]` section of `.loom.config` and runs it. With `--unlock`, integrates with a running `loomlocker` server to temporarily unlock secrets before running the command, then lets `loomlocker`'s auto-relock timer re-lock them.

**Why it exists**

Teams often have wrapper scripts for common workflows (render + deploy + check) that should be part of the loom toolchain rather than separate shell scripts. `execute` makes those first-class `loom` subcommands.

**When to use it**

When you have a multi-step workflow that you run repeatedly and want to centralise in `loom.toml`.

**Syntax**

```
loom execute <custom-command> [--unlock]
```

**Flags**

| Flag | Description |
|---|---|
| `--unlock` | If a loomlocker server is running, prompt for the session password and unlock secrets before running the command |

**Configuration in `.loom.config`**

```toml
[custom]
review-all = "loom fmt && loom ci && loom review"
ship = "loom weave --all --incremental && loom deploy"
```

**Examples**

```bash
loom execute review-all
loom execute ship --unlock
```

---

## Quick Reference Table

| Command | One-liner |
|---|---|
| `loom init` | Create project config and directory structure |
| `loom start` | Generate a starter library from your CLAUDE.md using an LLM |
| `loom thread prompt <Name>` | Scaffold a new prompt file |
| `loom thread block <Name>` | Scaffold a new block file |
| `loom thread overlay <Name>` | Scaffold a new overlay file |
| `loom weave <Name>` | Render a prompt to `dist/` |
| `loom weave --all` | Render all prompts |
| `loom cast <Name>` | Render and send to clipboard / stdout / file |
| `loom copy <Name>` | Render and copy to clipboard |
| `loom inspect` | Validate all source files |
| `loom trace <Name>` | Show inheritance chain and field sources |
| `loom unravel <Name>` | Print fully resolved fields without Markdown formatting |
| `loom contract <Name>` | Print contract and capabilities declarations |
| `loom doctor [Name]` | Health score and smell report (`--system`: check the installation) |
| `loom smells [Name]` | Heuristic quality issues |
| `loom stats [Name]` | Per-field token estimates |
| `loom minimize` | Find and remove duplicate content |
| `loom audit [Name]` | Scan resolved prompts for dangerous instructions |
| `loom blame <Name>` | Git attribution per field item |
| `loom changelog [Name]` | Prompt-centric git history |
| `loom diff` | Field-aware diff between prompts or against dist |
| `loom review` | PR review summary of all prompt changes |
| `loom ci` | Run all CI gates (inspect + doctor + check-lock + diff) |
| `loom lock` | Generate or update `loom.lock` fingerprints |
| `loom check-lock` | Verify fingerprints match `loom.lock` |
| `loom fingerprint <Name>` | Print the stable hash for a resolved prompt |
| `loom deploy` | Write all configured targets to their destinations |
| `loom test [Name]` | Smoke-test a prompt against a real AI model |
| `loom check-output <Name> <file>` | Validate a response file against a prompt contract |
| `loom list` | List all prompts and blocks |
| `loom fmt` | Format all `.loom` source files canonically |
| `loom graph [Name]` | Dependency graph; with a name, that prompt's neighbourhood |
| `loom impact <Name>` | Which prompts are affected when a prompt or block changes |
| `loom todos` | List all `todo:` items across the library |
| `loom stale` | Detect version mismatches between prompts and dependency files |
| `loom pack build` | Bundle the project into a `.lpack` archive |
| `loom pack install <path>` | Install a local `.lpack` archive |
| `loom pack list` | List installed packs |
| `loom pack remove <slug>` | Remove an installed pack |
| `loom install <slug>` | Install a pack from the registry |
| `loom publish <dir>` | Publish a pack to the registry |
| `loom mcp manifest [Name]` | Generate an MCP tool manifest |
| `loom import [file.md]` | Convert Markdown prompt to PromptLoom DSL |
| `loom completion <shell>` | Shell completion script (completes prompt names from the project) |
| `loom lsp` | Start the LSP server (for editors such as Neovim) |
| `loom summarize <path>` | LLM-generated summary of a project or files |
| `loom recipe list` | List built-in recipe templates |
| `loom recipe apply <recipe>` | Scaffold a library from a built-in recipe |
| `loom journal add <msg>` | Add a journal entry |
| `loom journal list [Name]` | List journal entries |
| `loom playground <Name>` | Live interactive prompt preview TUI |
| `loom execute <cmd>` | Run a custom command from `.loom.config` |
