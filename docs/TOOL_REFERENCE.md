# PromptLoom Tool Reference

> Last updated after: **Pack v2 Phases 7–9 + LOOM_COMMAND.md** — `loom inspect` v2, Lumine extension v2 DSL, `loom fmt` semantic simplification, complete CLI command reference

PromptLoom (`loom`) is a developer-first CLI that treats prompts like source code — with inheritance, block composition, validation, and Markdown rendering.

**Documentation:**
- `docs/LOOM_LANGUAGE.md` — DSL syntax reference (fields, operators, `from()`, packs, contracts)
- `docs/LOOM_COMMAND.md` — Complete CLI command reference (every flag, example, and use-case)
- `docs/PACKMAKER_DESIGN.md` — Pack v2 technical design and implementation phases

---

## Installation

```bash
# One-time install — builds and places the binary in ~/go/bin
go install github.com/sayandeepgiri/promptloom/cmd/loom

# ~/go/bin must be on your PATH (already added to ~/.zshrc):
# export PATH="$PATH:$HOME/go/bin"
```

After opening a new terminal (or running `source ~/.zshrc`), `loom` is available from any directory.

---

## Interactive REPL

Running `loom` with no arguments launches a full-screen interactive REPL:

```
  ██╗      ██████╗  ██████╗ ███╗   ███╗
  ██║     ██╔═══██╗██╔═══██╗████╗ ████║
  ...

  Treat prompts like source code    v0.1.0

  7 prompts  ·  5 blocks  ·  ✓ no errors

  Commands
  ──────────────────────────────────────────────
  weave <Name>    Render a prompt to Markdown
  ...

  ❯ _
```

| Key | Action |
|-----|--------|
| `Enter` | Execute command |
| `Tab` | Cycle through completions |
| `↑` / `↓` | Browse command history |
| `Escape` | Dismiss completions |
| `Ctrl+C` | Quit |

Tab completion works for subcommand names (`weave`, `inspect`, `list` …) and for prompt names after `weave`, `trace`, and `unravel`.

Special REPL-only commands: `clear` (clears output), `help` (shows command table), `exit` / `quit`.

---

## Project Setup

### `loom init`

Initialises a new PromptLoom project in the current directory.

```bash
loom init            # create loom.toml + empty prompts/, blocks/, dist/prompts/
loom init --sample   # also write the five spec example prompts and blocks
```

Creates:
- `loom.toml` — project configuration
- `prompts/` — prompt source files
- `blocks/` — reusable block files
- `dist/prompts/` — rendered Markdown output (written by `loom weave`)

---

## Configuration — `loom.toml`

```toml
[project]
name    = "my-prompts"
version = "0.1.0"

[paths]
prompts = "prompts"      # directory containing .prompt files
blocks  = "blocks"       # directory containing block .prompt files
out     = "dist/prompts" # output directory for rendered Markdown

[render]
default_format     = "markdown"
include_metadata   = false  # prepend YAML front matter to rendered files
include_source_map = false

[validation]
require_objective        = true
require_format           = true
require_contract         = false   # warn if no contract block
warn_on_empty_context    = true
warn_on_deep_inheritance = true
max_inheritance_depth    = 3
smell_constraint_limit   = 25      # threshold for Constraint Pile-Up smell
token_limit_warn         = 0       # warn when token estimate exceeds this (0 = off)
```

---

## DSL Syntax — `.prompt` Files

### Prompt declaration

```
prompt PromptName {
  fieldname:
    Field content here.
}
```

### Prompt with inheritance

```
prompt ChildName inherits ParentName {
  fieldname :=
    Replaces the parent's value.
}
```

### Block declaration

```
block BlockName {
  constraints:
    - Must not do X.
    - Always do Y.
}
```

### Using blocks inside a prompt

```
prompt MyPrompt inherits SomeParent {
  use BlockName

  context:
    Additional context here.
}
```

### Field names

| Field | Type | Description |
|---|---|---|
| `summary` | scalar | One-line description of the prompt |
| `persona` | scalar | Role / voice to adopt |
| `context` | scalar | Background information |
| `objective` | scalar | What to accomplish |
| `notes` | scalar | Free-form notes |
| `instructions` | list | Step-by-step directions |
| `constraints` | list | Hard rules |
| `examples` | list | Example inputs/outputs |
| `format` | list | Output format specification |

### Field operators

| Operator | Syntax | Behaviour |
|---|---|---|
| Define | `fieldname:` | Set field; warns if field already inherited (use `:=` to be explicit) |
| Override | `fieldname :=` | Unconditionally replace inherited value |
| Append | `fieldname +=` | Scalar: add new paragraph. List: append items |
| Remove | `fieldname -=` | List only: remove matching items by exact text |

---

## Commands

### `loom inspect` *(implemented)*

Validates all prompts and blocks in the project.

```bash
loom inspect
```

**Exit codes:**
- `0` — no errors
- `1` — validation error (unknown reference, cycle, bad field name, etc.)
- `2` — parse error (malformed `.prompt` file)

**Output example:**
```
prompts/child.prompt:1: Error: prompt "Child" inherits unknown prompt "NoSuchParent"

Validation complete.
  Prompts checked : 5
  Blocks checked  : 1
  Errors          : 1
  Warnings        : 0
```

**Validation checks:**
- Unknown parent / block references (with typo suggestion)
- Inheritance cycles
- Invalid field names
- `-=` on scalar fields
- Missing `objective` / `format` (configurable, soft warnings)
- Deep inheritance chains (configurable threshold)
- Ambiguous `:` redefine of an inherited field (warning)
- Empty `context` field (warning)

---

### `loom init` *(implemented)*

See [Project Setup](#project-setup) above.

---

### `loom weave` *(implemented)*

Resolves and renders prompts to Markdown.

```bash
# Single prompt — write to dist/prompts/<Name>.md
loom weave SpringBootReviewer

# Single prompt — print to stdout
loom weave SpringBootReviewer --stdout

# Single prompt — custom output path
loom weave SpringBootReviewer --out ./my-prompt.md

# All prompts — write to dist/prompts/
loom weave --all
```

**Markdown output structure** (sections skipped when empty):

```
[optional YAML front matter if include_metadata = true]

# PromptName

## Summary
...

## Persona
...

## Context
...

## Objective
...

## Instructions
- item

## Constraints
- item

## Examples
- item

## Output Format
- item

## Notes
...
```

> Note: the `format` field is rendered as `## Output Format`.

**Example output for SpringBootReviewer:**

```markdown
# SpringBootReviewer

## Summary
General-purpose engineering assistant prompt.

## Persona
You are a senior software engineer who writes clear, maintainable, production-ready code.

## Context
The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging.

## Objective
Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness.

## Instructions
- Read the code carefully.
- Identify correctness issues.
- Identify maintainability issues.
- Suggest practical improvements.

## Constraints
- Do not hallucinate APIs.
...
- Check retry and timeout behavior for external calls.

## Output Format
- Summary
- Issues Found
- Suggested Fixes
- Final Recommendation
```

**Optional YAML front matter** (enabled with `include_metadata = true` in `loom.toml`):

```yaml
---
name: SpringBootReviewer
inherits:
  - BaseEngineer
  - CodeReviewer
blocks:
  - SpringBootRules
---
```

---

### `loom trace <Name>` *(implemented)*

Shows the full inheritance chain, used blocks, and how every field was resolved — including every node that contributed to it in order.

```bash
loom trace SpringBootReviewer
```

**Example output:**
```
Prompt: SpringBootReviewer

Inheritance Chain:
  1. BaseEngineer
  2. CodeReviewer
  3. SpringBootReviewer

Used Blocks:
  - SpringBootRules

Resolved Fields:
  summary       defined by BaseEngineer
  persona       defined by BaseEngineer
  context       defined by SpringBootReviewer
  objective     defined by BaseEngineer → overridden by CodeReviewer → overridden by SpringBootReviewer
  instructions  appended by CodeReviewer
  constraints   defined by BaseEngineer → overridden by SpringBootRules
  format        defined by BaseEngineer → overridden by CodeReviewer
```

---

### `loom unravel <Name>` *(implemented)*

Prints the fully resolved prompt fields in plain text — no Markdown formatting, useful for debugging resolution before rendering.

```bash
loom unravel SpringBootReviewer               # raw field values
loom unravel SpringBootReviewer --with-source # show which node last set each field
```

**Example output (with --with-source):**
```
[summary       ]  (last set by: BaseEngineer)
  General-purpose engineering assistant prompt.

[objective     ]  (last set by: SpringBootReviewer)
  Review the Spring Boot implementation...

[constraints   ]  (last set by: SpringBootRules)
  - Do not hallucinate APIs.
  ...
```

---

### `loom list` *(implemented)*

Lists all prompts and blocks discovered in the project.

```bash
loom list            # show both prompts and blocks
loom list --prompts  # only prompts
loom list --blocks   # only blocks
```

**Example output:**
```
Prompts (4):
  BaseEngineer
  CodeReviewer                    inherits BaseEngineer
  SpringBootReviewer              inherits CodeReviewer
  TestWriter                      inherits BaseEngineer

Blocks (1):
  SpringBootRules
```

---

### `loom thread` *(implemented)*

Scaffolds a new prompt or block file with a starter template.

```bash
loom thread prompt MyPrompt                        # bare prompt
loom thread prompt MyPrompt --inherits BaseEngineer # prompt with inheritance
loom thread block  MyBlock                         # block stub
```

Files are created in the configured `prompts/` or `blocks/` directory. Name is converted to kebab-case for the filename (`SpringBootReviewer` → `spring-boot-reviewer.prompt`).

---

### `loom fmt` *(implemented)*

Rewrites all `.prompt` files to canonical formatting: consistent indentation, operator spacing, and blank lines between fields. Idempotent — running it twice produces no changes.

```bash
loom fmt           # format all .prompt files in place
loom fmt --check   # report unformatted files without modifying (exit 1 if any; useful in CI)
```

**What gets normalised:**
- Extra spaces in prompt/block declarations (`prompt Foo   inherits Bar` → `prompt Foo inherits Bar`)
- Operators with missing spaces (`fieldname:=` → `fieldname :=`)
- Missing blank lines between field declarations
- Blank line added between `use` group and first field

**Example:**

Before:
```
prompt Messy   inherits Base {
  objective:=
    Do the thing.
  constraints  +=
    - A constraint.
}
```

After `loom fmt`:
```
prompt Messy inherits Base {
  objective :=
    Do the thing.

  constraints +=
    - A constraint.
}
```

---

### `loom copy` *(implemented)*

Renders a prompt and copies the result to the clipboard (or another destination).

```bash
loom copy SecurityReviewer                     # copy to clipboard
loom copy SecurityReviewer --dest stdout       # print to stdout
loom copy SecurityReviewer --dest file         # write to dist/prompts/<Name>.md
loom copy SecurityReviewer --format json-anthropic
loom copy SecurityReviewer --with file:src/main.go
```

**Flags:** same as `loom weave` (`--set`, `--variant`, `--overlay`, `--with`, `--context`, `--format`, `--profile`).

---

### `loom cast` *(implemented)*

Like `loom copy` but with an explicit destination flag (`--to`), intended for piping into tools.

```bash
loom cast SecurityReviewer --to clipboard
loom cast SecurityReviewer --to stdout
loom cast SecurityReviewer --to file
```

---

### `loom weave --with` / `--context` *(implemented)*

Attach live file/git/stdin context to a rendered prompt.

```bash
loom weave CodeReviewer --with file:src/main.go        # attach file content
loom weave BugFixer --with git:diff                    # attach git diff
loom weave SecurityReviewer --with git:staged          # attach staged changes
git diff | loom weave CodeReviewer --with stdin        # attach piped input
loom weave SecurityReviewer --context SpringService    # load a named context bundle
loom weave SecurityReviewer --context SpringService --with git:staged
```

Context is appended as a `## Context` section at the end of the rendered prompt.

**Context bundle files** live in `contexts/<name>.context` and declare sources:

```toml
# contexts/SpringService.context
[[sources]]
type = "file"
path = "src/main/java/com/example/Application.java"

[[sources]]
type = "dir"
path = "src/main/java/com/example/service"
```

---

### `loom diff` *(implemented)*

Shows field-aware differences between two resolved prompts, or between a prompt and its last-rendered dist file.

```bash
loom diff PromptA PromptB                        # field-by-field diff of two prompts
loom diff SecurityReviewer --against-dist        # current vs dist/prompts/SecurityReviewer.md
loom diff --all --against-dist                   # all prompts vs dist
loom diff --all --against-dist --exit-code       # CI mode: exit 1 if any prompt is stale
loom diff PromptA PromptB --semantic             # semantic change classification
loom diff SecurityReviewer --against-dist --semantic
```

**Flags:**

| Flag | Description |
|---|---|
| `--against-dist` | Compare against the last-rendered dist file |
| `--all` | Diff all prompts (requires `--against-dist`) |
| `--exit-code` | Exit code 1 if any changes are detected (CI mode) |
| `--semantic` | Show semantic change classifications instead of line diff |

**Field-aware diff example:**

```
  Diff: SecurityReviewer vs CodeAssistant

  Objective
  ───────────────────────────────────────────────
  - Review code for security vulnerabilities.
  + Review code for correctness and maintainability.

  Constraints
  ───────────────────────────────────────────────
  - Check for SQL injection.
  + Check all authentication paths.
```

**Semantic diff example (`--semantic`):**

```
  Semantic diff: SecurityReviewer
  ───────────────────────────────────────────────

  constraint-removed  (high risk)
    - Check for SQL injection.

  objective-changed  (medium risk)
    - Review code for security vulnerabilities.
    + Review code for correctness and maintainability.
```

**Semantic change classes:**

| Field | Class | Risk |
|---|---|---|
| constraints added | `constraint-added` | medium |
| constraints removed | `constraint-removed` | high |
| format changed | `format-changed` | low |
| objective changed | `objective-changed` | medium |
| persona changed | `persona-changed` | low |
| instructions added | `capability-added` | low |
| instructions removed | `capability-removed` | medium |
| inheritance chain changed | `inheritance-changed` | high |
| summary/notes/context changed | `notes-updated` | low |
| examples changed | `examples-changed` | low |

---

### `loom review` *(implemented)*

Generates a Markdown PR summary of all prompt changes, suitable for pasting into a pull request description.

```bash
loom review                   # compare all prompts against their dist files
loom review --since HEAD~3    # compare current renders vs 3 commits ago
```

**Flags:**

| Flag | Description |
|---|---|
| `--since <git-ref>` | Compare current renders against the given git ref (e.g. `HEAD~3`, `main`) |

**Example output:**

```markdown
## PromptLoom Prompt Review

**Changed prompts:** SecurityReviewer, CodeAssistant

**SecurityReviewer**
- constraint-removed: Check for SQL injection removed (high risk)
- constraint-added: 2 new constraint(s) added

**CodeAssistant**
- notes-updated: minor wording change

**Risk summary:** 1 high-risk change(s). Review carefully before merging.
```

When `--since` is provided, `loom review` runs `git show <ref>:dist/prompts/<Name>.md` for each prompt and diffs against the current resolved render.

---

### `loom doctor` *(implemented)*

Runs structural checks and heuristic smell detection on one prompt or the whole library. Reports a health score (0–100).

```bash
loom doctor                     # check all prompts
loom doctor SecurityReviewer    # check one prompt
```

**Structural checks** (per prompt):

| Check | Behaviour |
|---|---|
| Parses cleanly | Always passes if the prompt is registered |
| Parent resolves | Error if parent prompt is missing |
| All blocks resolve | Error if any used block is missing |
| Dist file fresh | Warning if `dist/` file is older than source, or missing |
| Token limit | Warning if estimated tokens exceed `token_limit_warn` in `loom.toml` |
| Contract declared | Warning if `require_contract = true` and no contract block is defined |

Library-level check (shown when running `--all`):

| Check | Behaviour |
|---|---|
| Unused Blocks | Lists blocks not referenced by any prompt |

**Smell detectors:**

| Smell | Trigger |
|---|---|
| **Constraint Pile-Up** | More than `smell_constraint_limit` constraints (default: 25) |
| **God Prompt** | Objective has more than 5 sentences |
| **Output Ambiguity** | No `format` field declared |
| **Persona Soup** | Persona contains markers indicating multiple roles |
| **Duplicate Instructions** | Two instructions/constraints with ≥80% Jaccard word similarity |
| **Conflicting Instructions** | Contradictory phrases detected (e.g. "be brief" + "comprehensive") |
| **Format Drift** | Child prompt's format differs from parent's format |

**Health score bands:**

| Score | Band |
|---|---|
| 90–100 | Excellent |
| 75–89 | Good |
| 60–74 | Needs improvement |
| 40–59 | Risky |
| 0–39 | Poor |

**Example output:**

```
  loom doctor — SecurityReviewer

  Structural
  ────────────────────────────────────────────────
  ✓  Parses cleanly
  ✓  Parent resolves
  ✓  All blocks resolve
  ⚠  Dist file fresh  SecurityReviewer.md is stale (run loom weave)

  Prompt Health  82/100  Good
  ────────────────────────────────────────────────
  ⚠  Duplicate Instructions
      "Be precise." and "Give precise feedback." are 84% similar

  Smells: 1 warning(s) — run 'loom smells SecurityReviewer' for details
```

**`loom.toml` knobs for doctor:**

```toml
[validation]
require_contract       = false  # warn when no contract block is declared
smell_constraint_limit = 25     # threshold for Constraint Pile-Up smell
token_limit_warn       = 4096   # warn when a prompt exceeds this token estimate (0 = off)
```

---

### `loom smells` *(implemented)*

Standalone smell report — shows only the smell analysis without the structural checks.

```bash
loom smells                     # report all smells across the library
loom smells SecurityReviewer    # smells for one prompt
```

---

### `loom contract` *(implemented)*

Prints the `contract` and `capabilities` blocks declared in a prompt file.

```bash
loom contract BugFixPlanner
```

**Contract DSL syntax** (inside a `.prompt` file):

```
prompt BugFixPlanner inherits BaseAssistant {
  objective :=
    Analyze the bug and produce a fix plan.

  contract {
    required_sections:
      - Root Cause
      - Affected Files
      - Fix Plan
      - Risks
    forbidden_sections:
      - Full Rewrite
    must_include:
      - risk
    must_not_include:
      - production secret
      - api key
  }

  capabilities {
    allowed:
      - read_code
      - suggest_changes
    forbidden:
      - modify_production_code
      - delete_files
  }
}
```

**Example output:**

```
  Contract — BugFixPlanner

  Output Contract
  ────────────────────────────────────────────────
  Required sections:
    – Root Cause
    – Affected Files
    – Fix Plan
    – Risks
  Forbidden sections:
    – Full Rewrite
  Must include:
    – risk
  Must not include:
    – production secret
    – api key

  Capabilities
  ────────────────────────────────────────────────
  Allowed:
    – read_code
    – suggest_changes
  Forbidden:
    – modify_production_code
    – delete_files
```

---

### `loom check-output` *(implemented)*

Reads an output file (e.g. a model response) and validates it against the `contract` block declared in the named prompt. Exits `0` if all requirements pass, `1` if there are violations.

```bash
loom check-output BugFixPlanner response.md
```

**Validation rules:**

| Rule | Check |
|---|---|
| `required_sections` | Output must contain `## <Section>` heading |
| `forbidden_sections` | Output must NOT contain `## <Section>` heading |
| `must_include` | Output must contain the phrase anywhere (case-insensitive) |
| `must_not_include` | Output must NOT contain the phrase anywhere (case-insensitive) |

**Example output (pass):**

```
  check-output — BugFixPlanner
  response.md

  ✓ Output satisfies all contract requirements
```

**Example output (fail):**

```
  check-output — BugFixPlanner
  response.md

  ✗  required section "Affected Files" not found in output
  ✗  forbidden content "production secret" found in output

  2 contract violation(s) found
```

---

## Resolution Rules

The resolver walks the inheritance chain from the root ancestor to the target prompt. For each node in the chain it:

1. Applies each `use` block's fields
2. Applies the node's own field operations

**Scalar fields** (`summary`, `persona`, `context`, `objective`, `notes`):

| Operator | Result |
|---|---|
| `:` or `:=` | Replace current value |
| `+=` | Append as a new paragraph (`\n\n`) |
| `-=` | Not supported (validator error) |

**List fields** (`instructions`, `constraints`, `examples`, `format`):

| Operator | Result |
|---|---|
| `:` (in prompt) | Replace current list |
| `:=` | Replace current list |
| `+=` | Append items to current list |
| `-=` | Remove matching items by exact text |
| `:` (in block) | **Append** items — blocks are compositional by design |

---

## loom lock

Generate (or update) the `loom.lock` fingerprint file. The lockfile records a sha256 fingerprint for every resolved prompt and a sha256 hash of each block's source file.

```bash
loom lock          # writes / overwrites loom.lock
```

### What it writes

`loom.lock` is a TOML file placed in the project root:

```toml
[[prompts]]
name  = "CodeReviewer"
hash  = "a3f92b..."
blocks = ["SpringBootRules"]

[[prompts]]
name  = "SecurityReviewer"
hash  = "c7d01e..."
blocks = []

[[blocks]]
name = "SpringBootRules"
hash = "09ab23..."
```

- **Prompt hash** — sha256 of all resolved field values concatenated (identical to `loom fingerprint` output).
- **Block hash** — sha256 of the raw block source file bytes.

Commit `loom.lock` alongside your `.prompt` files. CI will detect any drift.

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Lockfile written successfully |
| 1 | Project or resolution error |

---

## loom check-lock

Verify that the current resolved state of all prompts and blocks matches the hashes recorded in `loom.lock`. Exits non-zero on any mismatch.

```bash
loom check-lock
```

### Output (clean)

```
✓ Lockfile matches current state
```

### Output (drift detected)

```
Lockfile mismatches detected:

  prompt  CodeReviewer
    locked:   a3f92b…
    current:  d84fc1…

  block   SpringBootRules
    locked:   09ab23…
    current:  55ee71…

Run `loom lock` to regenerate.
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All hashes match |
| 1 | One or more mismatches (or lockfile missing) |

---

## loom weave --all --incremental

Skip prompts whose resolved fingerprint is unchanged since the last weave. Useful in large libraries where most prompts haven't changed.

```bash
loom weave --all --incremental
```

### How it works

On each run, `loom` stores a per-prompt fingerprint in `.loom-cache` (project root, auto-generated, safe to gitignore). On the next `--incremental` run, prompts whose current fingerprint matches the cached value are silently skipped.

```
loom weave --all --incremental
✔  weaved  CodeReviewer        → dist/prompts/CodeReviewer.md
✔  weaved  SecurityReviewer    → dist/prompts/SecurityReviewer.md
⟳  skipped MigrationPlanner    (unchanged)
   ...
Woven: 2  Skipped: 5
```

### .loom-cache format

```toml
[hashes]
CodeReviewer = "a3f92b..."
SecurityReviewer = "c7d01e..."
```

Add `.loom-cache` to `.gitignore` — it is a local build artifact, not a contract.

---

## loom weave --all --watch

Re-render all prompts automatically whenever any `.prompt`, `.overlay`, or `.context` file changes. Useful during active prompt authoring.

```bash
loom weave --all --watch
```

`--watch` requires `--all` and cannot be combined with `--stdout` or `--out`.

### Behavior

- On startup, performs a full `weave --all` and prints results.
- Watches the `prompts/`, `blocks/`, and `overlays/` directories recursively.
- Changes are debounced (80 ms) so rapid editor saves trigger a single rebuild.
- Each rebuild prints a timestamped summary with elapsed time.
- Press **Ctrl+C** to stop.

```
[watch] initial build…
✔  weaved  CodeReviewer        → dist/prompts/CodeReviewer.md
✔  weaved  SecurityReviewer    → dist/prompts/SecurityReviewer.md
[watch] watching for changes — Ctrl+C to stop

[watch] change detected — rebuilding…
✔  weaved  CodeReviewer        → dist/prompts/CodeReviewer.md
[watch] rebuilt in 12ms
```

### Note

Watch mode is a **CLI-only** feature; it is not available inside the interactive REPL because it requires a blocking event loop.

---

## loom ci

Run all CI gates in sequence. Designed to be the single check you drop into a pull-request pipeline.

```bash
loom ci
```

### Gate sequence

| # | Gate | Equivalent command |
|---|------|--------------------|
| 1 | Syntax + reference validation | `loom inspect` |
| 2 | Health + smell analysis | `loom doctor` |
| 3 | Lockfile integrity | `loom check-lock` |
| 4 | Dist files not stale | `loom diff --all --against-dist` |

Gates run sequentially. All gates always run (no short-circuit), so you see the full picture on a first failure.

### Output (all passing)

```
CI Results
──────────────────────────────────────────────
✓ inspect      0 error(s), 3 warning(s)
✓ doctor       9 prompts checked — 9 healthy, 0 need attention
✓ check-lock   ✓ Lockfile matches current state
✓ diff         all dist files up-to-date

Status: PASSED
```

### Output (failure)

```
CI Results
──────────────────────────────────────────────
✓ inspect      0 error(s), 3 warning(s)
✓ doctor       9 prompts checked — 9 healthy, 0 need attention
✓ check-lock   ✓ Lockfile matches current state
✗ diff         stale dist files detected — run loom weave

Status: FAILED
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All gates passed |
| 1 | One or more gates failed |

### Typical CI configuration

```yaml
# GitHub Actions example
- name: PromptLoom CI
  run: loom ci
```

---

## Milestone 15 — Dependency Graph, Token Stats, and Local Pack System

### `loom graph` — Dependency graph

Visualise the full inheritance and block dependency tree.

```bash
loom graph                        # interactive terminal browser (default)
loom graph --no-interactive       # plain ASCII output
loom graph --format mermaid       # Mermaid diagram
loom graph --format dot           # Graphviz DOT source
loom graph SecurityReviewer       # subgraph for one prompt
loom graph --unused               # highlight blocks not used by any prompt
```

**Mermaid output** can be pasted directly into GitHub Markdown, Notion, or Obsidian.

### `loom stats` — Token estimates

Per-field token breakdown with percentage of total.

```bash
loom stats SecurityReviewer       # breakdown for one prompt
loom stats --all                  # all prompts sorted by total tokens
loom stats --all --limit 4096     # flag prompts exceeding the threshold
```

**Example output:**

```
  SecurityReviewer — token estimate

  Field               Tokens   %
  ──────────────────────────────────
  constraints            122   34%
  instructions            69   19%
  examples                52   14%
  persona                 26    7%
  context                 23    6%
  notes                   23    6%
  objective               16    4%
  format                  14    3%
  summary                 10    2%
  ──────────────────────────────────
  Total                  355
```

### `loom pack` — Local pack system

Bundle and share prompt libraries as versioned `.lpack` archives.

```bash
loom pack init                              # create pack.toml in the current project
loom pack build                             # bundle project into .lpack archive
loom pack install ./engineering-1.2.0.lpack # unpack into namespaced subdirs
loom pack list                              # list installed packs
loom pack remove engineering-essentials     # remove an installed pack
```

**`pack.toml`:**

```toml
[pack]
name        = "engineering-essentials"
version     = "1.2.0"
description = "Production-ready prompts for code review, testing, and security"
author      = "your-name"
license     = "MIT"
```

Installed packs go into `prompts/<pack-name>/` and `blocks/<pack-name>/`. Prompts can reference them with namespaced inheritance:

```
prompt MyReviewer inherits engineering-essentials/CodeReviewer {
  ...
}
```

Pack versions are recorded in `loom.lock` for reproducibility.

---

## Internal Architecture

```
.prompt files
    │
    ▼
Lexer  (internal/lexer)       — line-aware stateful tokenizer
    │
    ▼
Parser (internal/parser)      — builds []ast.Node from token stream
    │
    ▼
Registry (internal/registry)  — indexes prompts and blocks by name
    │
    ▼
Validator (internal/validate)  — checks refs, cycles, field rules
    │
    ▼
Resolver (internal/resolve)   — walks chain, applies blocks + ops → ResolvedPrompt
    │
    ▼
Renderer (internal/render)    — [Milestone 5] ResolvedPrompt → Markdown
    │
    ▼
TestRunner (internal/testrunner) — [Milestone 16] renders prompt → sends to AI model → checks contract
```

---

## Milestone 16 — Smoke Testing Against Real AI Models

`loom test` sends a rendered prompt to a configured AI model with a test fixture (or built-in stub) and checks the response against the prompt's declared `contract {}` block.

**Supported providers:**
- **Gemini** (default) — set `$GEMINI_API_KEY`
- **Anthropic** — set `provider = "anthropic"` and `$ANTHROPIC_API_KEY`

**Configuration in `loom.toml`:**

```toml
[testing]
provider      = "gemini"          # "gemini" or "anthropic"
api_key_env   = "GEMINI_API_KEY"  # env var holding the API key
default_model = "gemini-2.0-flash"
timeout_sec   = 30
```

---

### `loom test [Name]`

Run a single prompt's contract assertions against a real model.

```
loom test SecurityReviewer
loom test SecurityReviewer --model gemini-1.5-flash
```

**Output:**

```
  loom test

  ──────────────────────────────────────────────────

  ✓  SecurityReviewer      contract passed  (2.1s)
  ✗  BugFixer              missing section "Root Cause"  (1.4s)
  —  CodeReviewer          (no contract declared)

  ──────────────────────────────────────────────────
  Passed: 1 / 2  (skipped: 1)
```

**Flags:**

| Flag | Description |
|---|---|
| `--all` | Run tests for all prompts in the library |
| `--model <id>` | Override the model (e.g. `gemini-1.5-flash`) |
| `--record` | Record the response as a baseline for future `--compare` runs |
| `--compare` | Diff the current response against the recorded baseline |

**Test fixtures:**

Place input files at `tests/<PromptName>.input.md`. If no fixture exists, a built-in generic code review stub is used.

**Baselines:**

```bash
loom test SecurityReviewer --record    # writes tests/SecurityReviewer.baseline.md
loom test SecurityReviewer --compare   # asserts current response matches baseline contract
```

**CI integration:**

`loom ci` runs `loom test --all` as Gate 5. It skips gracefully when the API key env var is not set — CI always passes without a key.

**Exit codes:** `0` = all tested prompts passed, `1` = any failure or error.

---

## `.loomsecret` — Project API Key File

Every loom project can have a `.loomsecret` file in its root directory. It is loaded automatically by every `loom` command — no need to export env vars manually.

**Format:**

```
# .loomsecret — never commit this file
GEMINI_API_KEY=your-key-here
# ANTHROPIC_API_KEY=your-key-here
```

**Rules:**
- `KEY=VALUE` format, one per line; `#` lines are comments
- Shell-exported env vars always win over `.loomsecret` values
- `loom init` creates an empty `.loomsecret` template and adds it to `.gitignore` automatically
- File permissions are set to `0600` (owner-read-only) on creation

---

## Milestone 17 — Git Blame and Changelog

### `loom blame <Name>`

Shows git commit attribution for every resolved field item in a prompt. Traces each value back to the exact file, line, and git commit that last touched it.

```
loom blame SecurityReviewer
loom blame SecurityReviewer --field constraints
loom blame SecurityReviewer --instruction "Check for hardcoded secrets"
loom blame SecurityReviewer --since 2026-01-01
loom blame SecurityReviewer --since HEAD~10
```

**Example output:**

```
  SecurityReviewer — constraints

  ●  "Check for injection vulnerabilities (SQL, command, LDAP)."
       from:   blocks/security-checklist.block.loom  line 3
       origin: block composition
       commit: abc1234  by alice  2026-03-14
       msg:    "add injection checks to shared checklist"
```

**Flags:**

| Flag | Description |
|---|---|
| `--field <name>` | Limit to one field (e.g. `constraints`, `persona`) |
| `--instruction <text>` | Filter to items containing this text |
| `--since <date\|ref>` | Only show items changed after this date (`2026-01-01`) or ref (`HEAD~10`) |

**Notes:**
- Requires a git repository. Errors clearly if not in one.
- Files not tracked by git show `(untracked)` instead of commit info.
- Origin labels: `direct` (set in this prompt), `inherited` (from parent), `block composition` (from a `use` block).

---

### `loom changelog [Name]`

Scans git history for changes to `.loom` source files and presents a prompt-centric view of what changed, when, and by whom.

```
loom changelog
loom changelog SecurityReviewer
loom changelog --since HEAD~5
loom changelog --since 2026-04-01
loom changelog --format markdown
```

**Example output:**

```
  Prompt Changelog

  SecurityReviewer
  ──────────────────
  2026-04-15  Added block SpringBootRules  (alice)
  2026-04-02  Constraints += "Verify authentication on every endpoint."  (bob)
  2026-03-20  Output Format updated  (alice)
```

**Flags:**

| Flag | Description |
|---|---|
| `--since <date\|ref>` | Limit to commits after this date or ref |
| `--format markdown` | Emit Markdown output (suitable for piping to a file) |

**What it tracks per commit:**
- Inheritance changes (`Inheritance changed: BaseEngineer → CodeAssistant`)
- Block additions/removals (`Added block SpringBootRules`)
- List field changes (`Constraints += "..."`, `Constraints -= "..."`)
- Scalar field updates (`Persona updated`)
- Prompt created / deleted events

---

## Milestone 18 — Safety: Audit, Secret Slots, Env Separation

### `loom audit [Name]`

Scans resolved prompt fields for dangerous patterns: hardcoded secret references, policy-bypass instructions, destructive commands without confirmation, production credential references, PII without privacy qualifiers, and more.

```
loom audit
loom audit DeployAssistant
loom audit --all
```

**Example output:**

```
  loom audit

  ─────────────────────────────────────────────────────────

  BaseEngineer                              PASS
  DeployAssistant                           FAIL
  ─────────────────────────────────────────────────────────
  [HIGH]   constraints: "rm -rf /data without asking"
           Reason: destructive command without confirmation qualifier
           Fix: Add "only after user confirms" qualifier
```

**Exit codes:**
- `0` — all prompts clean
- `1` — at least one HIGH finding
- `2` — at least one MEDIUM finding (no HIGH)

**Risk levels and patterns scanned:**

| Risk | Pattern |
|---|---|
| HIGH | Hardcoded `.env`, `credentials`, `api_key=`, `api_secret=` references |
| HIGH | Safety bypass: `ignore policy`, `bypass validation`, `disregard previous` |
| HIGH | Destructive commands: `rm -rf`, `drop table`, `delete all` (without confirmation qualifier) |
| HIGH | Production references: `use production`, `production credentials` |
| MEDIUM | PII without privacy qualifier: `social security`, `ssn`, `credit card number` |
| MEDIUM | Removes confirmation gate: `without confirmation`, `no approval needed` |
| LOW | Urgency without safety qualifier: `as fast as possible`, `immediately execute` |

**Notes:**
- Also runs automatically as a gate in `loom ci`.
- Negation: if the same text contains a qualifier like `"with explicit confirmation"`, the HIGH finding is suppressed.

---

### Secret Slots (`slot name { secret: true }`)

Slots declared with `secret: true` cannot have their values provided via `--set` on the command line. This prevents plain-text secrets from appearing in rendered output or shell history.

```
# In a .loom file:
slot api_key { secret: true }

# At the CLI — this will error:
loom weave MyPrompt --set api_key=sk-abc123
# Error: slot "api_key" is marked secret and cannot be rendered as plain text.
# Pass secrets through the target tool's secure environment instead.
```

---

### `--env <name>` flag (weave, weave --all)

Applies an environment-specific block declared in the prompt with `env <name> { ... }`. Env blocks add field operations (typically `+=`) that layer stricter constraints for named environments like `prod` or `staging`.

```
loom weave GoEngineer --env prod --set repo_name=myrepo
```

**DSL syntax:**

```
prompt GoEngineer inherits BaseEngineer {
  env prod {
    constraints +=
      - All external calls must use timeouts and retries.
      - No debug logging in production paths.
  }
}
```

**Notes:**
- Env blocks are strictly additive — only `+=` semantics are applied.
- If `--env` names a block not declared on the prompt chain, an error is returned.
- `loom ci` runs the audit gate using the `prod` env when declared; falls back gracefully when not present.

---

## Milestone 19 — MCP Manifests and Import

### `loom mcp manifest [Name]`

Generates an MCP-compatible tool manifest from one or all prompts. Reads `contract {}` and `capabilities {}` blocks to produce a structured JSON tool definition.

```
loom mcp manifest
loom mcp manifest SecurityReviewer
loom mcp manifest --all --out .claude/mcp-prompts.json
```

**Example output:**

```json
{
  "tools": [
    {
      "name": "go-engineer",
      "description": "A base engineering assistant.",
      "inputSchema": {
        "type": "object",
        "properties": {
          "repo_name": { "type": "string" }
        },
        "required": ["repo_name"]
      },
      "capabilities": ["read_code", "suggest_changes", "run_tests"],
      "forbidden": ["modify_production_code", "delete_files"]
    }
  ]
}
```

**Flags:**

| Flag | Description |
|---|---|
| `--all` | Generate manifest for all prompts |
| `--out <path>` | Write manifest JSON to a file instead of stdout |

**How it maps DSL → JSON:**

| DSL construct | MCP field |
|---|---|
| `slot name {}` → | `inputSchema.properties` |
| Required slot (no default) | `inputSchema.required[]` |
| `capabilities { allowed: ... }` | `capabilities[]` |
| `capabilities { forbidden: ... }` | `forbidden[]` |
| `summary:` / `objective:` | `description` |

**Notes:**
- Warns when `capabilities {}` or `contract {}` are missing from a prompt.
- Does not execute prompts — reads metadata only.
- Prompt names are converted to kebab-case for the `name` field.

---

### `loom import [file.md]`

Heuristic Markdown parser that converts well-structured prompt Markdown files into PromptLoom `.loom` DSL files.

```
loom import old-prompts/CodeReviewer.md
loom import old-prompts/CodeReviewer.md --name CodeReviewer --out prompts/
loom import --dir old-prompts/ --out prompts/
```

**Example input (Markdown):**

```markdown
# CodeReviewer

## Persona
You are a senior software engineer with 10 years of experience.

## Instructions
- Review code for correctness.
- Check for edge cases.
- Suggest idiomatic alternatives.
```

**Example output (`.loom`):**

```
prompt CodeReviewer {
  persona:
    You are a senior software engineer with 10 years of experience.

  instructions:
    - Review code for correctness.
    - Check for edge cases.
    - Suggest idiomatic alternatives.
}
```

**Flags:**

| Flag | Description |
|---|---|
| `--name <Name>` | Prompt name (default: derived from filename) |
| `--out <dir>` | Output directory (default: `prompts/`) |
| `--dir <dir>` | Import all `.md` files from a directory |
| `--force` | Overwrite existing `.loom` files |

**Heading → field mapping:**

| Markdown heading | DSL field |
|---|---|
| `## Persona` | `persona:` |
| `## Summary` | `summary:` |
| `## Context` | `context:` |
| `## Objective` | `objective:` |
| `## Instructions` | `instructions:` |
| `## Constraints` | `constraints:` |
| `## Examples` | `examples:` |
| `## Format` / `## Output Format` | `format:` |
| `## Notes` | `notes:` |
| Any other heading | `notes:` (with a warning) |

**Notes:**
- Import is best-effort. Unrecognised sections are placed in `notes` with a warning.
- Never overwrites existing `.loom` files without `--force`.
- Automatically runs `loom inspect` after import and reports any issues.

---

## Milestone 20 — LSP Server and Editor Integration

### `loom lsp`

Starts a Language Server Protocol server on stdin/stdout (JSON-RPC 2.0 with Content-Length framing). Editors launch this automatically — it should not be run manually.

```
loom lsp
```

**LSP capabilities provided:**

| Feature | Detail |
|---|---|
| Diagnostics | Inline errors and warnings from `loom inspect`, updated on every change |
| Hover | Field documentation, operator semantics, and resolved prompt info |
| Go to definition | Jump from `inherits Name` / `use Name` to the declaration file |
| Completions | Field names + operators, prompt names after `inherits`, block names after `use` |
| Document symbols | All prompts, blocks, fields, and vars in the outline panel |

**Text sync:** Full (mode 1) — the server receives the full document text on every change.

**Neovim setup:** See `docs/neovim-lsp.md` for `nvim-lspconfig` and bare `vim.lsp.start` configs.

---

### Lumine — VS Code Extension

The **Lumine** VS Code extension (`promptloom-vscode`) provides first-class IDE support for `.loom` files. It runs standalone (no `loom lsp` dependency) using its own built-in TypeScript language analysis.

**Features:**
- Syntax highlighting (TextMate grammar for all Loom constructs)
- IntelliSense: field names, operators, prompt/block names, variable names, `loom.toml` keys
- Hover: field descriptions, operator semantics, prompt/block details
- Diagnostics: real-time errors and warnings matching `loom inspect`
- Go to definition, Find all references, Document symbols
- Auto-formatter matching `loom fmt` output
- Command palette: **Loom: Weave This Prompt**, **Loom: Inspect Library**, **Loom: Open Dependency Graph**
- File icons for `.prompt.loom`, `.block.loom`, `.overlay.loom`, `.vars.loom`
- Code snippets for all top-level constructs

**Extension settings:**

| Setting | Default | Description |
|---|---|---|
| `loom.loomExecutable` | `"loom"` | Path to the loom binary |
| `loom.validateOnSave` | `true` | Run validation on save |
| `loom.formatOnSave` | `false` | Auto-format on save |
| `loom.trace.server` | `"off"` | LSP trace level |

---

## Milestone 21 — Recipes, Interactive Weave, and Playground

### `loom recipe list`

Lists all built-in scaffolding recipes with descriptions and supported flags.

```
loom recipe list
```

**Built-in recipes:**

| Recipe | Description |
|---|---|
| `reviewer` | Code reviewer set: BaseEngineer, CodeReviewer, language/framework reviewer, SecurityReviewer, TestWriter |
| `api-designer` | API design set: APIDesigner, SchemaReviewer, ContractValidator |
| `migration-assistant` | Migration set: MigrationPlanner, CompatibilityChecker, RollbackPlanner |
| `security-auditor` | Security audit set: SecurityAuditor, DependencyReviewer, ThreatModeler |
| `docs-writer` | Documentation set: DocsWriter, READMEWriter, ChangelogWriter |

---

### `loom recipe apply <name>`

Scaffolds a prompt library from a built-in recipe template. Supports language/framework placeholders for the `reviewer` recipe.

```
loom recipe apply <name> [--language <lang>] [--framework <fw>] [--style <style>] [--force]
```

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--language` | `Generic` | Programming language (e.g. `go`, `rust`, `java`, `typescript`) |
| `--framework` | _(none)_ | Framework (e.g. `gin`, `axum`, `spring-boot`, `react`) |
| `--style` | `rest` | API style for `api-designer`: `rest` or `graphql` |
| `--force` | false | Overwrite existing files |

**Examples:**

```bash
loom recipe apply reviewer --language go --framework gin
loom recipe apply reviewer --language rust --framework axum
loom recipe apply api-designer --style graphql
loom recipe apply migration-assistant
loom recipe apply security-auditor
loom recipe apply docs-writer
```

The `reviewer` recipe with `--language rust --framework axum` creates:
- `prompts/BaseEngineer.prompt.loom`
- `prompts/CodeReviewer.prompt.loom`
- `prompts/RustAxumReviewer.prompt.loom`
- `prompts/SecurityReviewer.prompt.loom`
- `prompts/TestWriter.prompt.loom`
- `blocks/RustConventions.block.loom`
- `blocks/SecurityChecklist.block.loom`

After applying, `loom inspect` is run automatically to surface any issues.

---

### `loom weave --interactive`

Launches a guided TUI wizard for building a new prompt file step by step.

```
loom weave --interactive
```

**Wizard steps:**
1. **Base** — Choose an existing prompt to inherit from (or none)
2. **Blocks** — Multi-select blocks to include (Space to toggle)
3. **Variant** — Choose a starting variant (or none)
4. **Format** — Choose output format (markdown, json-anthropic, json-openai, etc.)
5. **Name** — Enter the new prompt name; file is written to `prompts/<Name>.prompt.loom`

**Navigation:** `↑↓`/`jk` navigate, `Enter` confirm, `Esc` go back, `q` quit.

---

### `loom playground <Name>`

Opens a full-screen interactive TUI for live previewing a prompt with real-time variant, format, overlay, and env controls.

```
loom playground <Name>
```

**Controls:**

| Key | Action |
|---|---|
| `1` | Scroll to top of preview |
| `2` | Copy rendered output to clipboard |
| `3` | Pick a variant |
| `4` | Pick a render format |
| `5` | Add an overlay |
| `6` | Set an env block |
| `7` | Reset all overlays and env |
| `8` | Save to `dist/prompts/<Name>.md` |
| `↑`/`k` | Scroll preview up |
| `↓`/`j` | Scroll preview down |
| `q` / `Ctrl+C` | Quit |

The header shows the active variant, env, and applied overlays. The stats bar shows token estimate, current format, and contract status.

---

## Milestone 22 — Maintenance Tools

### New DSL Fields: `todo:`, `kind:`, `compatible_with:`

Three new fields are now valid in both prompts and blocks:

```
prompt CodeReviewer {
  kind:
    code-review

  compatible_with:
    - Go
    - Rust
    - TypeScript

  todo:
    - Add more Go-specific idiom checks
    - Verify format output matches v2 spec
}
```

| Field | Type | Purpose |
|---|---|---|
| `kind:` | scalar | Categorises the prompt (e.g. `code-review`, `api-design`, `security`). Used by `loom inspect` to warn on kind–block mismatches. |
| `compatible_with:` | list | Documents which languages/frameworks/tools this prompt is designed for. |
| `todo:` | list | Inline improvement notes. Surfaced by `loom todos`. |

**`loom inspect` kind–block mismatch warning:** If a prompt declares `kind: X` and uses a block that declares `kind: Y` (where X ≠ Y), a warning is emitted.

---

### `loom minimize`

Detect and report redundant content across resolved prompts: exact duplicates, near-duplicates (Levenshtein), and contradictory constraint pairs.

```
loom minimize [PromptName] [--threshold <0-1>] [--apply]
```

**Flags:**

| Flag | Default | Description |
|---|---|---|
| `--threshold` | `0.85` | Minimum similarity ratio to flag as near-duplicate |
| `--apply` | false | Remove exact and near-duplicates from dist output (source files unchanged) |

**Finding types:**

| Type | Description |
|---|---|
| `exact-duplicate` | Identical list items after normalisation |
| `near-duplicate` | Items above the similarity threshold |
| `contradiction` | Opposing constraint patterns (e.g. "never use X" vs "always use X") |

---

### `loom stale`

Detect version mentions in prompt text that don't match the versions declared in dependency files.

```
loom stale [PromptName]
```

**Supported dependency files:** `go.mod`, `package.json`, `pom.xml`, `Cargo.toml`, `pyproject.toml`, `requirements.txt`

Example: if `pom.xml` declares Spring Boot `3.3.2` but a prompt says "using Spring Boot 2.7", a stale finding is reported.

---

### `loom todos`

List all `todo:` field items across the entire library (or a single prompt/block).

```
loom todos [PromptName]
```

---

### `loom journal`

A lightweight change journal stored in `.loom/journal/YYYY-MM-DD_<slug>.md` files.

#### `loom journal add <message>`

```
loom journal add "Refactored SecurityReviewer hierarchy" [--prompt <name>] [--author <name>] [--body <text>]
```

#### `loom journal list [PromptName]`

```
loom journal list                  # all entries, newest first
loom journal list CodeReviewer     # entries for a specific prompt
```

---

## Bug Fixes (post-M22)

### Kebab-case conversion for acronyms

The `toKebab` / `toKebabCase` helpers previously inserted a dash before **every** uppercase letter, so `MLFeatureEngineer` became `m-l-feature-engineer` in MCP manifests and scaffolded filenames.

The fix: a dash is only inserted before an uppercase letter when the previous character is lowercase **or** the next character is lowercase (i.e., only at word boundaries). Consecutive uppercase runs (acronyms) are kept together.

| Before | After |
|---|---|
| `m-l-feature-engineer` | `ml-feature-engineer` |
| `a-p-i-security-reviewer` | `api-security-reviewer` |

Affected: `loom mcp manifest` (name field), `loom thread` (filename), REPL prompt completion.

---

### Semantic diff — `format-changed` and `examples-changed` display

The `--semantic` flag on `loom diff` previously showed all list items in `format-changed` and `examples-changed` blocks with `+` (green), even if they were removed from the left prompt.

The fix: removed items are now prefixed internally so the renderer displays them with `-` (red) and added items with `+` (green), matching the behaviour of `constraint-added`/`constraint-removed`.

---

## Milestone 23 — Workspace Intelligence and Quest Mode

### `loom start`

Generates a tailored PromptLoom starter library by reading project context files and (by default) calling an LLM.

**Prerequisites:** `loom.toml` must exist (run `loom init` first). `CLAUDE.md` must be present in the project directory.

```
loom start                    # LLM generation, moderate token budget
loom start --minimal          # LLM generation, 3-5 prompts only
loom start --best             # LLM generation, maximum quality and coverage
loom start --nollm            # no LLM — uses built-in templates for detected stack
loom start --nollm --stack go # override stack detection
```

#### Workflow

1. Scans workspace: reads `CLAUDE.md` (required) and `TODO.md` (optional).
2. Detects tech stack from `go.mod`, `package.json`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, etc.
3. Generates a plan (file list with descriptions).
4. Shows plan and prompts: **[y] Generate  [e] Edit plan  [n] Cancel**.
5. If `e`: opens plan in `$EDITOR` for free-form editing, then re-parses.
6. Writes files to the configured `paths.prompts` and `paths.blocks` directories (overwrites existing files).

#### Token Tiers

| Flag | Files generated | Token budget |
|---|---|---|
| `--minimal` | 3-5 prompts + 1 block | Low |
| (default) | 8-12 prompts + 2-4 blocks | Moderate |
| `--best` | 15-20 prompts + 5-8 blocks | High (180 s timeout) |

#### Stack Detection

Automatically detected stacks: `go`, `python`, `typescript`, `javascript`, `rust`, `java-spring`, `java`.

Detection logic:

| File present | Detected stack |
|---|---|
| `go.mod` | go |
| `pyproject.toml` / `requirements.txt` | python |
| `tsconfig.json` + `package.json` | typescript |
| `package.json` | javascript |
| `Cargo.toml` | rust |
| `pom.xml` with spring-boot | java-spring |
| `pom.xml` | java |
| (none) | universal fallback |

#### `--nollm` Built-in Templates

When `--nollm` is used, stack-specific templates are applied:

| Stack | Templates included |
|---|---|
| `go` | BaseGoEngineer, GoCodeReviewer, GoTestWriter, GoDocWriter, SecurityReviewer + GoConventions block |
| `python` | BasePythonEngineer, PythonCodeReviewer, PythonTestWriter + PythonConventions block |
| `typescript` / `javascript` | BaseNodeEngineer, NodeCodeReviewer, NodeTestWriter + NodeConventions block |
| `rust` | BaseRustEngineer, RustCodeReviewer + RustConventions block |
| `java` / `java-spring` | BaseJavaEngineer, JavaCodeReviewer, JavaTestWriter + JavaConventions block |
| (unknown) | BaseEngineer, CodeReviewer, TestWriter, DocWriter |

#### Plan Edit Format

When `e` is chosen, the plan is opened as a text file with this format:

```
## FileName.prompt.loom | Short description
prompt FileName {
  ...
}

## BlockName.block.loom | Short description
block BlockName {
  ...
}
```

Lines starting with `#` are comments. Each `##` header starts a new file entry.

#### Internal Packages

| Package | Purpose |
|---|---|
| `internal/workspace` | Scans project directory; detects stack, reads CLAUDE.md and TODO.md |
| `internal/starter` | Plan type, plan format/parse, file writer |
| `internal/starter` (llm.go) | LLM API calls (Gemini and Anthropic) for plan generation |
| `internal/starter` (templates.go) | Embedded no-LLM templates per stack |

---

### `loom summarize`

Generates a structured Markdown summary of your project or specific files using an LLM. The summary is suitable for attaching to prompts via `--with file:`.

```
loom summarize workspace          # whole-project architecture summary
loom summarize src/               # specific directory
loom summarize main.go utils.go   # specific files
loom summarize src/ tests/        # multiple paths at once
```

#### Workspace mode

`loom summarize workspace` always saves the output to `.loom/context/architecture-summary.md`. It generates sections: Overview, Tech Stack, Key Directories, Entry Points, Testing, Configuration, Notable Patterns.

Context is built from: file tree, file counts per directory, and key file contents (README.md, CLAUDE.md, go.mod, package.json, Cargo.toml, pom.xml, Makefile, Dockerfile, loom.toml). Large files are truncated at 3 KB; total context is capped at 40 KB.

#### Path mode

`loom summarize <path...>` reads specified files and directories and produces a concise summary covering: purpose, key components, dependencies, patterns, and gotchas. File content capped at 8 KB each; total at 50 KB.

#### Flags

| Flag | Description |
|---|---|
| `--save` | Save output to `.loom/context/<name>-summary.md` |
| `--out <path>` | Write output to a specific file path |

#### REPL multi-select file picker

In the REPL, typing `summarize #` opens a **file picker** (not the prompt picker). This picker supports multi-select:

| Key | Action |
|---|---|
| `↑` / `↓` | Navigate items |
| `Space` | Toggle item selection (checkmark appears) |
| `Enter` | Confirm — inserts all selected items |
| `Esc` | Cancel picker |
| Type | Filter items by name |

Items shown: `workspace` (whole project), top-level directories with file counts, root-level files with sizes.

Example flow:
```
> summarize #src     ← type to filter
  ▶ ✓ src/          (42 files)   ← Space to select
    ✓ tests/        (18 files)   ← Space to select
[Enter] → summarize src/ tests/
```

#### After generation

The saved file can be attached to any prompt:
```
loom copy CodeReviewer --with file:.loom/context/architecture-summary.md
```

#### Internal Packages

| Package | Purpose |
|---|---|
| `internal/summarize` | Core summarization logic — file tree builder, LLM call, output writing |


---

## `loom publish`

Uploads a local vault directory (containing `vault.toml` + `.loom` files) to the PromptLoom registry.

```
loom publish <pack-dir> [--registry <url>] [--secret <token>] [--dry-run]
```

### Pack Directory Layout

```
my-pack/
  vault.toml                    ← required: vault metadata
  BaseEngineer.prompt.loom
  Conventions.block.loom
  CodeReviewer.prompt.loom
  ...
```

### `vault.toml` Format

```toml
[vault]
name        = "Go Backend"
slug        = "go-backend"          # URL-safe identifier, used in loom install
version     = "1.0.0"
description = "Prompt pack for Go backend development."
author      = "your-name"
tags        = ["go", "backend"]
```

### Flags

| Flag | Description |
|---|---|
| `--registry <url>` | Override registry base URL (also `$LOOM_REGISTRY_URL`) |
| `--secret <token>` | Upload secret (also `$UPLOAD_SECRET`) |
| `--dry-run` | Preview what would be uploaded without sending |

### File type inference

| File suffix | Uploaded as |
|---|---|
| `.block.loom` | `block` |
| `.overlay.loom` | `overlay` |
| any other `.loom` | `prompt` |

---

## `loom install`

Downloads a named vault (prompt-pack) from the PromptLoom registry, writes the raw `.loom` source files, and compiles them to rendered `.md` files.

```
loom install <vault-name> [--registry <url>]
```

### Directory Layout

After `loom install go-backend`, the following tree is created under the current working directory:

```
loompack/
  go-backend/
    source/           ← raw .loom files exactly as stored in the registry
      base-go-engineer.prompt.loom
      go-conventions.block.loom
      ...
    compiled/         ← rendered Markdown, one .md per prompt
      BaseGoEngineer.md
      GoCodeReviewer.md
      ...
    pack.json         ← vault metadata + install timestamp
```

### Flags

| Flag | Description |
|---|---|
| `--registry <url>` | Override registry base URL (also `$LOOM_REGISTRY_URL`) |

### Environment variables

| Variable | Default | Description |
|---|---|---|
| `LOOM_REGISTRY_URL` | `https://registry.promptloom.dev` | Registry base URL |

### Compilation

The source `.loom` files are compiled locally using the same parse → register → resolve → render pipeline as `loom weave`. Each prompt in the vault produces one `.md` file in `compiled/`. Files that fail to parse are skipped silently — the source is always written first.

### Internal Packages

| Package | Purpose |
|---|---|
| `internal/installer` | Fetch bundle, write source files, compile to Markdown, write `pack.json` |
| `internal/cli/install.go` | Cobra command wiring and output formatting |

---

## Registry Server (`server/`)

The PromptLoom registry is a standalone Go HTTP service in `server/`. It stores vaults and their `.loom` files in PostgreSQL and exposes a REST API.

### API Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/vaults` | List all vaults |
| `GET` | `/api/v1/vaults/{slug}` | Get vault metadata |
| `GET` | `/api/v1/vaults/{slug}/bundle` | Download full vault bundle (JSON) |
| `POST` | `/api/v1/vaults` | Upload / replace a vault (requires `X-Upload-Secret` header) |
| `DELETE` | `/api/v1/vaults/{slug}` | Delete a vault (requires `X-Upload-Secret` header) |
| `GET` | `/healthz` | Health check |

### Bundle JSON Format

The bundle endpoint returns the vault metadata plus all source files:

```json
{
  "name": "Go Backend",
  "slug": "go-backend",
  "version": "1.0.0",
  "description": "Prompt pack for Go backend development",
  "author": "sayandeep",
  "tags": ["go", "backend"],
  "files": [
    {
      "path": "base-go-engineer.prompt.loom",
      "file_type": "prompt",
      "content": "prompt BaseGoEngineer { ... }"
    }
  ]
}
```

### Configuration (`.env`)

Copy `server/.env.example` to `server/.env` and fill in values:

| Variable | Description |
|---|---|
| `DATABASE_URL` | PostgreSQL connection URL |
| `PORT` | HTTP port (default `8080`) |
| `UPLOAD_SECRET` | Shared secret for upload/delete operations |
| `CORS_ORIGINS` | Allowed CORS origins (`*` for all) |

### Database Setup

```bash
# Apply schema
psql "$DATABASE_URL" -f server/internal/db/schema.sql
```

### Running the server

```bash
cd server
cp .env.example .env   # fill in DATABASE_URL etc.
go run .
```

### Server Internal Packages

| Package | Purpose |
|---|---|
| `server/internal/db` | Connection pool (`pgxpool`) + schema file |
| `server/internal/models` | `Vault`, `VaultFile`, `Bundle`, `ListItem` types |
| `server/internal/store` | CRUD — `ListVaults`, `GetVault`, `GetBundle`, `UpsertVault`, `DeleteVault` |
| `server/internal/handlers` | HTTP handlers, CORS middleware |

---

## LoomLocker (`loomlocker` binary)

A separate binary that protects secrets during AI-assisted development sessions by replacing real values with random tokens. Lives in `loomlocker/`.

### Quick start

```bash
cd loomlocker && go build -o ~/.local/bin/loomlocker ./cmd/loomlocker

# In your project directory (where .loom.config lives):
loomlocker start          # prompts for password, starts server + REPL
```

### Interactive REPL commands

| Command | Description |
|---|---|
| `lock` | Replace all secret values with `lk_*` tokens |
| `unlock` | Re-enter password to restore original values |
| `status` | Show locked state, file list, secret count |
| `stop` | Unlock (if needed) and shut down |
| `help` | Show command list |

### Client commands (from another terminal)

| Command | Description |
|---|---|
| `loomlocker lock` | Lock secrets via HTTP |
| `loomlocker unlock` | Unlock via HTTP (prompts password) |
| `loomlocker status` | Show current state |
| `loomlocker stop` | Stop server (prompts password if locked) |

### `.loom.config` reference

```json
{
  "secret": [
    ".loom.secret",
    ".env:{DB_PASSWORD}",
    "application.yaml:{kafka.consumer-id}"
  ],
  "loomlocker": {
    "active": true,
    "lockhost": "http://localhost",
    "port": "8053",
    "recoverable": false,
    "unlock_duration_seconds": 10
  },
  "custom": {
    "runproject": "python server.py",
    "testproject": "pytest"
  }
}
```

### Secret entry formats

| Format | Effect |
|---|---|
| `".loom.secret"` | Lock ALL key=value pairs in the file |
| `".env:{KEY}"` | Lock only the value of KEY |
| `"app.yaml:{a.b.c}"` | Lock YAML key at dotted path `a → b → c` |

### Token format

Locked values look like `lk_7f3a9b2c1d4e5a6b` (recognizable, safe to inspect).

### Crypto

- Non-recoverable (default): random AES-256 key in memory; if server crashes while locked, restore from git
- Recoverable: Argon2id key derivation from password → AES-256-GCM → `.loom.secret.lock`
- Password verification: bcrypt (cost=12), in-memory only

### HTTP API

Base: `http://localhost:{port}/api`

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/ping` | none | Health check |
| `GET` | `/status` | none | Detailed state |
| `POST` | `/lock` | none | Lock all secrets |
| `POST` | `/unlock` | `{"password":"..."}` | Unlock + start timer |
| `POST` | `/autolock` | none | Immediate relock (for language libs) |
| `POST` | `/stop` | password if locked | Unlock + shutdown |

### Internal packages

| Package | Purpose |
|---|---|
| `loomlocker/internal/config` | `.loom.config` JSON parser, walk-up finder |
| `loomlocker/internal/crypto` | AES-256-GCM, Argon2id key derivation, bcrypt, random tokens |
| `loomlocker/internal/locker` | `State` (in-memory mapping), env/YAML file lock/unlock logic |
| `loomlocker/internal/server` | HTTP server, lock/unlock state machine, auto-relock timer |
| `loomlocker/internal/repl` | Interactive REPL loop |
| `loomlocker/cli` | Cobra commands: `start`, `lock`, `unlock`, `stop`, `status` |

---

## `loom execute`

Run a custom shell command defined in `.loom.config → custom`. With `--unlock`, temporarily restores secrets for the startup window.

```
loom execute <custom-command> [--unlock]
```

### Flow with `--unlock`

1. Check if loomlocker is running (`GET /api/ping`)
2. If running and **locked**: prompt for password (masked) → `POST /api/unlock`
3. If running and **already unlocked**: skip (idempotent)
4. If **not running**: skip unlock, run normally
5. Run the shell command
6. Loomlocker's auto-relock timer handles re-locking independently

### Example

```bash
# .loom.config has: "custom": { "runproject": "python server.py" }
loom execute runproject --unlock
# → prompts password → unlocks → runs python server.py → auto-relocks in 10s
```

---

## Phase 3 — New Workspace Structure

### New `loom init` workspace layout

`loom init` now creates a structured `loom/` workspace directory instead of placing files at the project root:

```
loom/
  src/
    prompts/     ← .prompt.loom files
    blocks/      ← .block.loom files
    overlays/    ← .overlay.loom files
  loompack/      ← installed packs (loom install)
  context/
    docs/        ← context files (REPO.md, TODO.md, etc.)
    REPO.md      ← project context for AI sessions
    TODO.md      ← current tasks
  .export.loom   ← export rules
  .dependency.loom ← pack dependency declarations
  .loom.env      ← environment variable stubs (not committed)
  .loom.config   ← server connection config (not committed)
  .loom.secret   ← API keys (not committed, .gitignore'd)
```

`loom.toml` is created in the project root with paths pointing into `loom/src/`.

`.gitignore` is updated to exclude `loom/.loom.secret`, `loom/.loom.config`, and `loom/loompack/`.

`loom install` auto-detects the workspace: if `loom/` exists, packs go to `loom/loompack/`; otherwise the legacy `loompack/` path is used.

---

### `.export.loom` — Export rules

Declares which prompt files to include when publishing a vault.

**Location:** `loom/.export.loom`

**Syntax:**

```
export `pkg`
export `pkg` match `glob`
export `pkg` match `glob` except `file`
export `pkg` except match `glob`
```

**Example:**

```
export `go-backend`
export `reviewers` match `*Reviewer*`
export `security-pack` match `*.block.loom` except `internal-rules.block.loom`
```

**Rules:**
- Bare `export \`pkg\`` — include all `.loom` files in the source directories
- `match \`glob\`` — restrict to files matching the glob pattern
- `except match \`glob\`` — exclude files matching the glob
- `except \`file\`` — exclude a specific file

**Internal package:** `internal/export`

| Function | Description |
|---|---|
| `ParseFile(path)` | Parse `.export.loom` → `[]Rule` |
| `Rule.Match(baseDir)` | Apply glob rules → list of matching file paths |
| `WriteDefault(path)` | Write empty template to a new project |

---

### `.dependency.loom` — Pack dependencies

Declares remote vault dependencies for a project. Format mirrors `requirements.txt`.

**Location:** `loom/.dependency.loom`

**Syntax:**

```
# one dependency per line; comments with #
go-backend==1.0.0
security-essentials>=0.3.0
reviewers~=2.1
```

**Supported operators:** `==`, `>=`, `>`, `<=`, `<`, `~=`

**Checking installed state:**

`loom inspect` reads `.dependency.loom` and warns when a declared dependency is not found in `loompack/` (or `loom/loompack/`).

**Internal package:** `internal/deps`

| Function | Description |
|---|---|
| `ParseFile(path)` | Parse `.dependency.loom` → `[]Dependency` |
| `Installed(packDir)` | Scan `loompack/` → `map[slug]version` |
| `Missing(deps, installed)` | Return deps not satisfied by installed set |
| `WriteDefault(path)` | Write empty template to a new project |

---

## Phase 4 — Language Libraries

Client libraries for integrating LoomLocker into application startup. All libraries read `.loom.config` (walking up the directory tree) and fall back to `LOOM_HOST` / `LOOM_PORT` env vars. If loomlocker is not running, all operations are transparent no-ops.

### bloompy (Python)

**Location:** `libs/bloompy/`  
**Install:** `pip install bloompy` or `pip install -e libs/bloompy`  
**Requires:** Python 3.8+, optional `requests` (falls back to stdlib `urllib`)

```python
from bloompy import Safe
from dotenv import load_dotenv

Safe().unlock().execute(load_dotenv).autolock()

# Context manager form:
with Safe().unlock() as safe:
    load_dotenv()
# autolock called automatically on __exit__

app.run()
```

**API:**

| Method | Description |
|---|---|
| `Safe(config=None)` | Auto-detect config from `.loom.config` or env vars |
| `.silent()` | Suppress log output |
| `.unlock(password=None)` | Unlock; falls back to `LOOM_SESSION_PASSWORD` env var |
| `.execute(fn)` | Call `fn()` — always runs regardless of lock state |
| `.autolock()` | Signal startup complete → trigger immediate relock |
| `with Safe().unlock() as safe:` | Context manager; autolock on exit |

---

### gloom (Go)

**Location:** `libs/gloom/`  
**Module:** `github.com/sayandeepgiri/promptloom/libs/gloom`  
**Requires:** Go 1.22+, no external dependencies

```go
import "github.com/sayandeepgiri/promptloom/libs/gloom"

func main() {
    gloom.NewSafe().
        Unlock().           // LOOM_SESSION_PASSWORD fallback
        Execute(func() {
            godotenv.Load()
        }).
        Autolock()

    server.Start()
}
```

**API:**

| Function / Method | Description |
|---|---|
| `NewSafe()` | Auto-detect config from `.loom.config` or env vars |
| `WithConfig(cfg)` | Explicit config |
| `(*Safe).Silent()` | Suppress log output |
| `(*Safe).Unlock(password...)` | Unlock; falls back to `LOOM_SESSION_PASSWORD` |
| `(*Safe).Execute(fn func())` | Run `fn` — always called |
| `(*Safe).Autolock()` | Signal startup complete → immediate relock |

---

### loomj (Java)

**Location:** `libs/loomj/`  
**Artifact:** `dev.promptloom:loomj:0.1.0`  
**Requires:** Java 11+ (`java.net.http.HttpClient`), no external dependencies

```java
import dev.promptloom.loomj.Safe;

public class App {
    public static void main(String[] args) {
        new Safe()
            .unlock()                    // LOOM_SESSION_PASSWORD fallback
            .execute(() -> Dotenv.load())
            .autolock();

        server.start();
    }
}
```

**API:**

| Method | Description |
|---|---|
| `new Safe()` | Auto-detect config from `.loom.config` or env vars |
| `new Safe(LockerConfig)` | Explicit config |
| `.silent()` | Suppress log output |
| `.unlock(String... password)` | Unlock; falls back to `LOOM_SESSION_PASSWORD` |
| `.execute(Runnable fn)` | Run `fn` — always called |
| `.autolock()` | Signal startup complete → immediate relock |

**Config (`LockerConfig`):**

| Method | Description |
|---|---|
| `LockerConfig.fromEnv()` | Load from `.loom.config` + env var overrides |
| `getHost()` / `getPort()` | Connection settings |
| `baseUrl()` | `host:port/api` |

---

### Common configuration

All three libraries resolve config in the same order:

1. `.loom.config` found by walking up from the working directory (JSON, `loomlocker.lockhost` / `loomlocker.port`)
2. `LOOM_HOST` / `LOOM_PORT` environment variables (override file values)
3. Built-in defaults: `http://localhost:8053`

Password resolution order for `unlock()`:

1. Argument passed directly to `unlock()`
2. `LOOM_SESSION_PASSWORD` environment variable
3. Skip — secrets stay locked, `execute` still runs normally

---

## Pack v2 Phase 3 — DSL Extensions

### Multiple Inheritance

Prompts can now inherit from multiple parents:

```
prompt Combined inherits ReviewerA, ReviewerB {
  instructions := from(parent[*])
}
```

- `parent[0]` = first named parent (`ReviewerA`)
- `parent[1]` = second named parent (`ReviewerB`)
- If a field is defined in multiple parents and the child has no `:=` for it: first parent wins + warning (Phase 4 resolver)

Namespaced parents (installed packs):

```
prompt MyPrompt inherits go-backend.GoCodeReviewer {
}
```

### Namespaced `use`

Block references in `use` now accept `slug.BlockName`:

```
prompt Foo {
  use go-backend.GoConventions
}
```

### `from()` Expression Language

The `from()` expression is used on the RHS of `:=` to compose values from parents.

| Syntax | Meaning |
|---|---|
| `from(parent[*])` | All items from all parents |
| `from(parent[0])` | From first parent only |
| `from(parent[1])` | From second parent only |
| `from(pack.Name)` | From a specific named parent |
| `parent[0].instructions[*]` | Explicit field + subscript from parent 0 |
| `parent[0].instructions[0..5]` | Items 0–4 from parent 0 |
| `from(parent[*]) and { ... }` | All parents + additional inline items |
| `from(parent[0]) and from(parent[1])` | Explicit two-parent merge |

**Subscript notation:**

| Notation | Meaning |
|---|---|
| `[*]` | All items |
| `[N]` | Single item at 0-based index N |
| `[N..M]` | Items N..(M-1), exclusive end |

**Inline literal block:**

```
instructions := from(parent[*]) and {
  - new instruction 1
  - new instruction 2
}
```

**Type rules (enforced at resolve time in Phase 4):**
- `from(parent[*])` on a scalar field = type error (vector on scalar)
- `and { ... }` always produces a vector
- `from(parent[0])` on a scalar = valid (single source)

---

## Pack v2 Phase 4 — Resolver v2 (Multi-Parent Resolution)

### Recursive Resolver

The resolver was rewritten from a linear chain-walk to a recursive model (`resolveNode`). Each parent is fully resolved before the child merges from it, enabling true multi-parent DAG traversal.

### Multi-Parent Merge Semantics

When a prompt inherits multiple parents and does **not** use `from()` for a field:

- **First-parent-wins**: the resolved value from `parent[0]` is used
- A warning is recorded in `ResolvedPrompt.Warnings` when two or more parents both define the same field

When a prompt uses `from()`, explicit merge control overrides first-parent-wins.

### Deduplication

After all `from()` expressions are evaluated and list items collected, exact-string-match duplicates are removed (first occurrence kept). This applies to all list fields: `instructions`, `constraints`, `examples`, `format`.

### `InheritsChain`

For multi-parent prompts, `InheritsChain` contains all ancestor names from all parent branches, deduplicated, in a breadth-first order, with the child prompt last. Example: `["A", "B", "C"]` for `C inherits A, B`.

### Cycle Detection

Cycle detection uses an `inProgress` map keyed by `resolvedNamespace:promptName`. Diamond inheritance (two parents sharing a common grandparent) is handled correctly — the grandparent is resolved once and reused.

### Namespace-Aware Resolution

When resolving a prompt from an installed pack (e.g. `go-backend.GoCodeReviewer`), bare parent names inside that prompt are resolved within the `go-backend` namespace first, then fall back to local.

### `AllEnvBlocks`

`ResolvedPrompt.AllEnvBlocks` accumulates all `env { }` blocks from the full inheritance chain (root-first). This is used by `ResolveWithOptions` when `opts.Env` is set to find and apply the matching env block.

### Type Errors at Resolve Time

| Expression | Field type | Result |
|---|---|---|
| `from(parent[*])` | scalar | error: vector on scalar field |
| `from(parent[0])` | scalar | valid |
| `from(parent[*])` | list | valid |
| `and { ... }` | scalar | error |
| `and { ... }` | list | valid |

---

## Pack v2 Phase 5 — `loom install` Recursive Dependency Resolution

### `loom install <slug>`

`loom install` now performs recursive transitive dependency installation:

1. Fetches and installs the requested pack
2. Reads the pack's `.dependency.loom`
3. For each dependency not already installed at a satisfying version → installs recursively
4. Detects version conflicts across the full dependency graph
5. Writes / updates `loompack.lock` with exact installed versions

### `.dependency.loom` — `as` alias support

```
webdev==0.0.1 as dev
mypack as mp
```

The `as <alias>` clause scopes the namespace alias to the declaring pack's internal prompts only.

### Version Constraint Operators

| Operator | Meaning |
|---|---|
| `==1.0.0` | Exact version |
| `>=1.0.0` | Minimum version |
| `>1.0.0` | Greater than |
| `<=2.0.0` | Maximum version |
| `<2.0.0` | Less than |
| `~=1.4.2` | Compatible release: `>=1.4.2` and same `major.minor` (1.4.x) |
| _(none)_ | Any version |

### `loompack.lock`

Written to `loompack.lock` in the project root after every `loom install`. Records exact installed versions and which pack required each dependency.

```toml
# loompack.lock — generated by loom install; do not edit manually

[[pack]]
slug = "go-backend"
version = "1.0.0"

[[pack]]
slug = "python"
version = "2.1.0"
required_by = ["go-backend"]
```

### Conflict Detection

If two packs require incompatible versions of the same dependency (e.g. `a` needs `shared>=2.0.0` and `b` needs `shared<2.0.0`), `loom install` prints all conflicts and exits 1. The `loompack.lock` is still written with the installed state for manual inspection.

### CLI Output

Transitive dependencies are shown with a `↳` prefix and `(transitive dependency)` label. The tip and file listings are only shown for the directly requested pack.

### Packages Involved

| Package | Role |
|---|---|
| `internal/deps/deps.go` | `.dependency.loom` parsing, `ParseContent`, `as` alias |
| `internal/deps/version.go` | Semver parsing and constraint checking (`Satisfies`) |
| `internal/deps/packlock.go` | `loompack.lock` read/write, `Upsert`, `Find` |
| `internal/installer/recurse.go` | `InstallWithDeps`, conflict detection, recursive traversal |

---

## Phase 7 — `loom inspect` Updates (v2 Validation)

`loom inspect` was updated to handle multiple inheritance, namespace-qualified references, and static validation of `from()` expressions.

### Multi-Parent Validation

Each entry in the `inherits` list is now validated independently. If a prompt inherits two unknown parents, two separate error messages are emitted (one per unknown parent). Only bare (non-namespaced) names get "Did you mean?" suggestions.

```
Error: prompt "Child" inherits unknown prompt "NoSuchA"
Error: prompt "Child" inherits unknown prompt "NoSuchB"
```

Cycle detection now walks the full multi-parent graph. Diamond inheritance (`D inherits B, C; B inherits A; C inherits A`) is correctly identified as cycle-free.

### Namespace-Qualified References

Block and prompt references of the form `pack-slug.Name` are resolved against installed packs via the namespace registry. An error is emitted only if the slug is installed but the name is not found there, or if the slug is not installed at all. Bare names that fall through namespace lookup also get typo suggestions.

### `from()` Static Validation

The following are caught statically without running the resolver:

| Error | Example |
|---|---|
| `from(parent[*])` on a scalar field | `persona := from(parent[*])` when `persona` is scalar |
| `from(parent[N])` index out of bounds | `from(parent[5])` on a single-parent prompt |
| `from(parent[N..M])` range out of bounds | `from(parent[0..10])` on a 2-parent prompt |
| Named ref not in parents | `from(OtherPrompt)` when `OtherPrompt` is not declared in `inherits` |
| Unknown field in `from()` | `parent[0].badfield[*]` |
| `from()` in a block | Blocks have no parents; `from()` is meaningless |

### Deprecated Operator Warnings

`+=` and `-=` in prompts now emit deprecation warnings:

```
Warning: prompt "Foo" field "instructions": '+=' is deprecated in v2 — use ':= from(parent[*]) and { ... }' instead
Warning: prompt "Foo" field "items": '-=' is deprecated in v2 and has no direct replacement — flag for manual resolution
```

These are warnings (not errors), so existing packs continue to compile. The `examples/legacy/` directory contains old-syntax packs that will produce these warnings.

### Updated Helper Functions

| Function | Change |
|---|---|
| `detectCycle` | DFS over full `n.Parents` slice (multi-parent) |
| `inheritanceDepth` | Returns max depth across all parent chains |
| `hasInheritedField` | Walks all ancestors recursively via multi-parent edges |
| `allAncestorFields` | Accepts `[]string` parent names; walks full ancestry |
| `checkBlock` | Reports error if a `from()` expression appears in a block field |

---

## Phase 8 — Lumine VS Code Extension (v2 DSL Support)

Phase 8 updates the Lumine language server extension to fully support the v2 DSL: multi-parent inheritance, `from()` expression language, new fields (`kind`, `todo`, `compatible_with`), and deprecation warnings for `+=`/`-=`.

### Parser Changes (`src/server/parser.ts`)

- `PROMPT_RE` updated to capture comma-separated multi-parent list: `inherits A, B, C`
- `LoomNode` now has `parents: string[]` and `parentRanges: Range[]` (multi-parent). The existing `parent` and `parentRange` fields remain as backward-compat aliases to `parents[0]` / `parentRanges[0]`
- `FIELD_OP_RE` extended to recognize `kind`, `todo`, `compatible_with`
- `FieldOp` gains optional `fromExprRaw?: string` — populated when `:=` RHS starts with `from(` or `parent[`

### Registry Changes (`src/server/registry.ts`)

- `inheritanceChain()` rewritten as BFS over `node.parents[]` — returns flat deduplicated list of all ancestors
- `hasCycle()` rewritten as DFS over `node.parents[]` using `onPath` + `visited` sets

### Validator Changes (`src/server/validator.ts`)

- Check #3 (unknown parent): loops over `node.parents[]`, uses per-parent `parentRanges[i]` for error location
- Check #5 (inheritance cycle): uses `node.parents.length > 0` guard
- Check #10 (ambiguous `:`): merges field sets from all parents
- Check #10 (deep inheritance): uses `node.parents.length > 0` guard
- **New check #11**: deprecated `+=` emits warning `'+=' is deprecated in v2 — use ':= from(parent[*]) and { ... }' instead`; `-=` emits `'-=' is deprecated in v2 and has no direct replacement`
- **New check #12**: `from(parent[*])` on a scalar field (`summary`, `persona`, `context`, `objective`, `notes`, `kind`) emits an error
- `SCALAR_FIELDS` and `ALL_VALID_FIELDS` updated to include `kind`, `todo`, `compatible_with`

### Syntax Highlighting (`syntaxes/loom.tmLanguage.json`)

- `prompt-declaration` pattern updated to allow comma-separated multi-parent list and `.` in names
- `field-declaration` pattern updated to include `kind`, `todo`, `compatible_with`
- New `from-expression` rule highlights `from(parent[*|N|N...M])` with distinct scopes for `from` keyword, `parent`, and subscript
- New `from-and-keyword` rule highlights `and` before `{` as an operator keyword

### Completion Provider (`src/providers/completion.ts`)

- `SCALAR_FIELDS` gains `kind`; `LIST_FIELDS` gains `todo` and `compatible_with`
- `inherits` trigger regex updated to `/\binherits\s+[a-zA-Z0-9_./-]*(?:\s*,\s*[a-zA-Z0-9_./-]*)*$/` for multi-parent completions
- `OP_DETAIL` for `+=` and `-=` prefixed with `⚠ deprecated —`
- Deprecated operators sorted below modern ones via `sortText: 'zz_...'`
- New `from()` completions trigger when cursor is on a line matching `fieldname :=` (empty RHS): offers `from(parent[*])`, `from(parent[0])`, `from(parent[*]) and { ... }`, and `from(parent[N...M])` as snippet completions

### Hover Provider (`src/providers/hover.ts`)

- `FIELD_DOCS` updated with `kind`, `todo`, `compatible_with`
- `FIELD_OP_RE` updated to include new fields
- `INHERITS_RE` updated to match full multi-parent comma list
- `nodeDeclHover`: shows all parents as `Inherits: **A**, **B**, **C**`
- `promptNameHover`: shows all parents on "Inherits from" line
- Inherited-by calculation uses `node.parents[]` instead of `node.parent`
- Hover on `inherits` clause works for any parent in multi-parent list (second+)
- New hover for `from` keyword explains the expression language

### Definition Provider (`src/providers/definition.ts`)

- `INHERITS_RE` updated to match full multi-parent list
- Go-to-definition on `inherits` clause works for any parent in a comma-separated list (checks `inherits` keyword exists before cursor position)

### Snippets (`snippets/loom.json`)

| Prefix | Description |
|---|---|
| `promptmi` | Prompt with multi-parent inheritance (`inherits A, B`) |
| `frompall` | `from(parent[*])` — all items from all parents |
| `frompone` | `from(parent[N])` — items from one parent |
| `frompadd` | `from(parent[*]) and { ... }` — merge and add |
| `overlay` | Updated to use v2 `:= from(parent[*]) and { ... }` style |

---

## Phase 9 — `loom fmt` Semantic Simplification of `from()` Expressions

`loom fmt` was updated to properly serialize and semantically simplify `from()` AST nodes. Previously the formatter only re-emitted raw source lines; now it rebuilds `from()` output from the parsed AST, enabling canonical formatting and simplification.

### Multi-Parent Inherits Formatting

`loom fmt` now serializes multi-parent `inherits` lists correctly:

```
# Input (irregular spacing)
prompt Child  inherits   A,   B {

# Output (canonical)
prompt Child inherits A, B {
```

### Canonical `from()` Serialization

`from()` expressions are rebuilt from the AST into canonical form:

| Expression type | Canonical output |
|---|---|
| Single `from(parent[*])` | `from(parent[*])` on one line |
| Single `from(parent[N])` | `from(parent[N])` on one line |
| Named ref | `from(PromptName)` on one line |
| With literal block | `from(parent[*]) and {` / items / `}` multi-line |

Indentation is always consistent: expression at field-body indent, literal items at +2, closing `}` at field-body indent.

### Semantic Simplifications

Three simplifications are applied automatically, all safe to perform without registry context:

| Rule | Before | After |
|---|---|---|
| **Remove empty literal** | `from(parent[*]) and {}` | `from(parent[*])` |
| **Deduplicate adjacent identical units** | `from(parent[0]) and from(parent[0])` | `from(parent[0])` |
| **Collapse single-element range to index** | `from(parent[0..1])` | `from(parent[0])` |

Distinct units are preserved: `from(parent[0]) and from(parent[1])` is left as-is.

### Packages Involved

| Package | Role |
|---|---|
| `internal/format/format.go` | `formatFromExpr`, `formatFromUnit`, `formatSubscript`, `simplifyFromExpr`, `fromUnitsEqual` |
