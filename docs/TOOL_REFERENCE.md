# PromptLoom Tool Reference

> Last updated after: **Milestone 15** — dependency graph, token stats, and local pack system (V2 complete)

PromptLoom (`loom`) is a developer-first CLI that treats prompts like source code — with inheritance, block composition, validation, and Markdown rendering.

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
```
