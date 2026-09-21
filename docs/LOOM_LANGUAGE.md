# The Loom Prompt Language — Complete Reference

> **For:** developers writing `.loom` prompt files from scratch  
> **File format version:** v2 (current)  
> **CLI:** `loom`

---

## What is a loom prompt?

A loom prompt is a source file that describes the instructions you want to give an AI model. Instead of writing a flat wall of text, you organise the prompt into named fields, inherit from base prompts, and compose reusable rule blocks — exactly like writing code.

The toolchain resolves all inheritance, applies composition rules, and renders the result to a clean Markdown file you can hand to any LLM.

---

## 1. File Types and Naming

| File extension | Contains | Example |
|---|---|---|
| `.prompt.loom` | A prompt declaration | `GoCodeReviewer.prompt.loom` |
| `.block.loom` | A reusable rule block | `GoConventions.block.loom` |
| `.overlay.loom` | A modifier applied at render time | `terse.overlay.loom` |

All three kinds live inside a pack's directory structure:

```
my-pack/
  prompts/
    BaseEngineer.prompt.loom
    GoCodeReviewer.prompt.loom
  blocks/
    GoConventions.block.loom
  overlays/
    terse.overlay.loom
  .metadata.loom
  .dependency.loom
  .export.loom
```

For local project prompts (no pack), create any directory and point `loom.toml` at it. The same file extensions apply.

---

## 2. Prompt Structure

Every prompt file starts with a `prompt` keyword, a name (PascalCase by convention), and a body in braces.

```
prompt MyPrompt {
  ... fields and declarations ...
}
```

A minimal working prompt:

```
prompt Greeter {
  persona :=
    You are a friendly assistant.

  objective :=
    Help the user with their request clearly and concisely.
}
```

---

## 3. Fields

Prompts are built from a fixed set of named fields. Each field has a type: **scalar** (one string) or **list** (ordered set of strings).

### Scalar fields

| Field | Purpose |
|---|---|
| `summary` | One-sentence description of the prompt (shown in `loom list`) |
| `persona` | Who the AI is — role, expertise, voice |
| `context` | Situation the AI is operating in |
| `objective` | The primary goal of this prompt |
| `notes` | Free-form notes for the prompt author (not rendered to the LLM) |

### List fields

| Field | Purpose |
|---|---|
| `instructions` | Ordered steps the AI must follow |
| `constraints` | Hard rules the AI must not break |
| `examples` | Input/output examples |
| `format` | How to structure the response |

### Writing scalar fields

Put the value indented below the field name:

```
persona :=
  You are a senior Go engineer with deep knowledge of idiomatic Go,
  concurrency patterns, and the standard library.
```

Multi-line values are fine — just keep them indented consistently.

### Writing list fields

Each item is an indented line starting with `- `:

```
instructions :=
  - Read the full context before responding.
  - Suggest tests alongside any code changes.
  - Explain your reasoning step by step.
```

### The only operator: `:=`

In v2, `:=` is the only field operator. It sets the field value for this prompt. If the prompt inherits from a parent, `:=` unconditionally replaces whatever the parent had.

```
prompt Child inherits Parent {
  persona :=
    You are a specialised reviewer.   ← completely replaces Parent's persona
}
```

> **Why only `:=`?** The old `+=` and `-=` operators created subtle ordering bugs when inheritance chains grew. The new `from()` expression language (section 8) gives you explicit, readable control over how parent values are composed.

---

## 4. Inheritance

A prompt can inherit from one or more parent prompts. Every field from every ancestor is resolved before the child's own fields are applied.

### Single inheritance

```
prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review the submitted code for correctness and idiomatic style.

  instructions :=
    - Check for unchecked errors.
    - Verify all exported functions are documented.
    - Suggest table-driven tests for any new logic.
}
```

If a field is defined in `BaseEngineer` and `CodeReviewer` doesn't use `:=` on it, the parent's value is used as-is.

### Multiple inheritance

```
prompt FullStackReviewer inherits BackendReviewer, FrontendReviewer {
  summary :=
    Reviews both backend and frontend changes in a single pass.
}
```

- `parent[0]` = `BackendReviewer`
- `parent[1]` = `FrontendReviewer`

**First-parent-wins rule:** If both parents define the same field and the child has no `:=` for it, `parent[0]`'s value is used. The resolver also records a warning so you can be explicit with `from()` if you want a different merge strategy.

### Namespaced parents (cross-pack)

When inheriting from an installed pack, prefix the prompt name with the pack slug:

```
prompt MyReviewer inherits go-backend.GoCodeReviewer {
  instructions :=
    from(parent[0]) and {
      - Also check for missing context propagation.
    }
}
```

---

## 5. Blocks

A block is a reusable set of field operations. It is not a full prompt — it has no persona, no objective, just rules you want to mix into any prompt that `use`s it.

### Defining a block

```
block GoConventions {
  constraints :=
    - Follow standard Go formatting (gofmt / goimports).
    - Return errors as the last return value; never panic in library code.
    - Use context.Context as the first argument for functions that may block.
    - Prefer concrete types over interface types unless abstraction is warranted.
    - Use table-driven tests with t.Run for subtests.
}
```

### Using a block in a prompt

```
prompt GoCodeReviewer inherits BaseGoEngineer {
  use GoConventions

  objective :=
    Conduct a thorough, constructive code review on the submitted Go code.
}
```

Blocks are applied after parent resolution and before the prompt's own fields. Multiple blocks are applied in `use` order.

### How blocks and overlays combine

Blocks and overlays are *composable*: what they define is **added** to what the prompt already has, so mixing in several never silently drops rules.

| Where the field is written | List fields (`instructions`, `constraints`, `examples`, …) | Scalar fields (`persona`, `objective`, …) |
|---|---|---|
| Inside a **block** or **overlay** | `:=` **adds** to the existing items | `:=` replaces |
| In the **prompt's own** body | `:=` replaces | `:=` replaces |
| In a **variant** or **env** block | `:=` replaces (use `from()` to extend) | `:=` replaces |

Exception: **`format`** describes the single shape of the answer, so with `:=` the *last writer wins* — a "JSON only" overlay replaces the prompt's format instead of extending it.

Order: parents → blocks (in `use` order) → the prompt's own fields → variant → overlays → env. After everything is applied, exact-duplicate list items are removed (first occurrence kept).

Consequence to remember: if a prompt `use`s a block **and** writes its own `constraints :=`, its own list replaces the block's, because a prompt's own fields are applied last. To keep the block's rules, don't redefine that field in the prompt.

### Namespaced block usage

```
prompt MyPrompt {
  use go-backend.GoConventions
}
```

---

## 6. Variables and Slots

Variables let you parameterise a prompt at resolve time. Slots are variables that must be supplied before the prompt can render.

### `var` — optional with a default

```
prompt BaseEngineer {
  var language = "Python"
  var experience_level = "senior"

  persona :=
    You are a {{ experience_level }} {{ language }} engineer.
}
```

Override at the CLI: `loom weave BaseEngineer --var language=Go`

### `slot` — required, must be supplied

```
prompt RepoReviewer {
  slot repo_name { required: true }
  slot team_name { required: true }

  context :=
    You are reviewing the {{ repo_name }} repository, maintained by {{ team_name }}.
}
```

If a slot has no default and is not supplied, `loom weave` prompts for it interactively.

### Secret slots

For sensitive values (API keys, tokens) that should not appear in rendered output:

```
prompt APIClient {
  slot api_key { secret: true }

  notes :=
    Use {{ api_key }} when constructing authenticated requests.
}
```

Secret values are substituted but redacted from any trace or diff output.

### Template syntax

Use `{{ var_name }}` anywhere in a field value:

```
objective :=
  Help the user write idiomatic {{ language }} code in the {{ repo_name }} repository.
```

Unresolved tokens (placeholders with no matching var/slot) are recorded in `ResolvedPrompt.UnresolvedTokens` and flagged by `loom inspect`.

---

## 7. Variants

A variant is a named set of field overrides that can be activated at render time.

```
prompt CodeAssistant inherits BaseEngineer {
  instructions :=
    - Read the request carefully before responding.
    - Suggest minimal changes.

  variant strict {
    constraints :=
      - Never suggest external libraries.
      - All suggestions must include tests.
      - Do not refactor code that was not part of the original request.
  }

  variant exploratory {
    instructions :=
      from(parent[0]) and {
        - Feel free to suggest alternative architectures.
        - Highlight trade-offs between approaches.
      }
  }
}
```

Activate at the CLI: `loom weave CodeAssistant --variant strict`

Variants only affect the fields they declare; all other resolved fields are unchanged.

---

## 8. Environment Blocks

An `env` block holds field operations applied only when a specific environment is active. Useful for tightening constraints in production without duplicating the whole prompt.

```
prompt DataPipelineEngineer inherits BaseEngineer {
  instructions :=
    - Validate all input schemas before processing.
    - Emit structured logs for every pipeline stage.

  env prod {
    constraints :=
      from(parent[0]) and {
        - All external calls must use timeouts.
        - No debug logging in production paths.
        - Treat all user data as PII unless explicitly classified otherwise.
      }
  }

  env staging {
    notes :=
      Verbose logging is allowed. Destructive operations require a confirmation prompt.
  }
}
```

Activate at the CLI: `loom weave DataPipelineEngineer --env prod`

---

## 9. The `from()` Expression Language

When a child prompt needs to compose (not just replace) parent values, use a `from()` expression on the right-hand side of `:=`.

### Pull all items from all parents

```
instructions :=
  from(parent[*])
```

Merges the `instructions` lists from every parent. Exact-string duplicates are removed after merging (first occurrence kept).

### Pull from a specific parent by index

```
persona :=
  from(parent[0])
```

Copies the `persona` scalar from the first parent. This is the correct way to pick one value when you have multiple parents.

### Pull from all parents, then add more items

```
instructions :=
  from(parent[*]) and {
    - Also check for context propagation issues.
    - Flag any goroutine that is not guarded by a WaitGroup or channel.
  }
```

The `and { ... }` block appends literal items after the merged parent values.

### Explicit per-parent merge

```
instructions :=
  from(parent[0]) and from(parent[1])
```

Equivalent to `from(parent[*])` for two parents, but lets you reorder or selectively omit.

### Field subscript — pull a slice of a parent's list

```
instructions :=
  parent[0].instructions[1..4] and parent[1].instructions[*]
```

| Subscript | Meaning |
|---|---|
| `[*]` | All items |
| `[2]` | Single item at 0-based index 2 |
| `[1..4]` | Items at indices 1, 2, 3 (exclusive end) |

### Pull from a named parent (slug.Name)

```
persona :=
  from(go-backend.BaseGoEngineer)
```

The named prompt must be a declared parent in the `inherits` list.

### Type rules

| Expression | Field kind | Result |
|---|---|---|
| `from(parent[*])` | scalar | **Error** — `[*]` is a vector; use `[0]` instead |
| `from(parent[0])` | scalar | Valid |
| `from(parent[*])` | list | Valid |
| `from(parent[0]) and from(parent[1])` | scalar | **Error** — `and` always produces a vector |
| `and { ... }` | scalar | **Error** |

---

## 10. Contracts

A `contract` block declares assertions about the rendered output. The `loom weave --enforce-contract` flag checks these at render time.

```
prompt SummaryWriter {
  objective :=
    Produce a structured summary of the provided document.

  contract {
    required_sections:
      - Summary
      - Key Points
      - Action Items

    must_include:
      - "Summary"

    must_not_include:
      - "I cannot"
      - "As an AI"
  }
}
```

| Directive | Meaning |
|---|---|
| `required_sections` | Section headings that must appear in the rendered output |
| `forbidden_sections` | Section headings that must not appear |
| `must_include` | Literal strings the output must contain |
| `must_not_include` | Literal strings the output must not contain |

Section matching is exact: `required_sections: - Summary` is satisfied by a Markdown heading line of any level (`# Summary`, `## Summary`, `### Summary:`), ignoring case, but **not** by `## Summary of findings` or by the words "## summary" inside a sentence. `must_include` / `must_not_include` are case-insensitive substring checks.

---

## 11. Capabilities

A `capabilities` block declares which actions the prompt is allowed and forbidden from performing. Consumed by host applications that gate tool calls.

```
prompt CodeAssistant {
  capabilities {
    allowed:
      - read_code
      - suggest_changes
      - run_tests

    forbidden:
      - modify_production_code
      - delete_files
      - access_secrets
  }
}
```

Capability names are free-form strings — define your own vocabulary and enforce it in your host application.

---

## 12. Overlays

An overlay is a thin modifier applied at render time without touching the base prompt source. Use them for stylistic variations (terse vs verbose, formal vs casual) that you want to apply ad-hoc across many prompts.

### Defining an overlay

```
overlay terse {
  constraints :=
    - Keep all responses under 200 words unless the user asks for more.
    - Omit preamble and caveats. Lead with the answer.

  format :=
    - One-paragraph answer
    - Code block (if applicable)
}
```

### Applying an overlay at the CLI

```
loom weave GoCodeReviewer --overlay terse
```

Multiple overlays are applied in order:

```
loom weave GoCodeReviewer --overlay terse --overlay json-output
```

### Overlays from an installed pack

```
loom weave MyPrompt --overlay go-backend.terse
```

---

## 13. Pack Structure

A pack is a distributable directory of prompts, blocks, and overlays with metadata.

```
my-pack/
  prompts/
    BaseEngineer.prompt.loom     ← one prompt per file
    GoCodeReviewer.prompt.loom
  blocks/
    GoConventions.block.loom     ← one block per file
  overlays/
    terse.overlay.loom           ← one overlay per file
  .metadata.loom                 ← pack identity (JSON)
  .dependency.loom               ← this pack's own pack dependencies
  .export.loom                   ← which names are public API
```

### `.metadata.loom`

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "slug": "go-backend",
  "version": "1.0.0",
  "name": "Go Backend",
  "author": "yourname",
  "description": "Prompt pack for Go backend development.",
  "tags": ["go", "backend"]
}
```

Required: `id` (UUID v4), `slug`, `version`, `name`.

The `slug` is the namespace prefix used in cross-pack references: `go-backend.GoCodeReviewer`.

### `.dependency.loom`

Declare other packs this pack depends on:

```
go-foundation>=1.0.0
testing-utils==2.3.0
shared-rules~=1.4.0
webdev==0.0.1 as dev
```

Operators: `==`, `>=`, `>`, `<=`, `<`, `~=` (compatible release).  
The `as <alias>` clause scopes a namespace alias to this pack's internal prompts only.

### `.export.loom`

List the names that are public API. Referencing a non-exported name from outside the pack produces a warning at `loom inspect`.

```
export `GoCodeReviewer`
export `GoConventions`
```

Names not listed here are internal implementation details of the pack.

---

## 14. Full Worked Example

Here is a complete, realistic pack illustrating everything from sections 2–13.

### `blocks/EngineeringDefaults.block.loom`

```
block EngineeringDefaults {
  constraints :=
    - Do not hallucinate APIs, functions, or libraries.
    - Do not suggest solutions you are not confident about.
    - Always prefer idiomatic solutions for the language in use.
    - Prefer the standard library over third-party packages when the standard library is sufficient.
}
```

### `prompts/BaseEngineer.prompt.loom`

```
prompt BaseEngineer {
  use EngineeringDefaults

  var language = "Python"
  slot repo_name { required: true }

  summary :=
    Foundational engineering assistant for {{ repo_name }}.

  persona :=
    You are a senior {{ language }} engineer with 10 years of experience.
    You write clean, testable, and well-documented code.

  context :=
    The user is working on the {{ repo_name }} codebase in a production environment
    with automated tests and CI/CD.

  objective :=
    Help the user write, review, and debug {{ language }} code with a focus
    on correctness, idiomatic style, and maintainability.

  instructions :=
    - Read the full context before responding.
    - Ask for clarification when the request is ambiguous.
    - Suggest tests alongside any code changes.
    - Explain your reasoning step by step.

  format :=
    - Analysis
    - Proposed Changes
    - Tests

  contract {
    required_sections:
      - Proposed Changes
    must_not_include:
      - "I cannot help"
      - "As an AI"
  }
}
```

### `prompts/GoCodeReviewer.prompt.loom`

```
prompt GoCodeReviewer inherits BaseEngineer {
  use GoConventions

  var language = "Go"

  summary :=
    Conducts thorough, constructive Go code reviews.

  persona :=
    You are a principal Go engineer conducting a thorough code review.
    You care deeply about correctness, idiomatic style, and long-term maintainability.

  objective :=
    Review the submitted Go code and provide actionable feedback focused on
    correctness, idiomatic patterns, and test coverage.

  instructions :=
    from(parent[0]) and {
      - Check for unchecked errors and silent failures.
      - Flag goroutine leaks and missed context cancellations.
      - Identify data races in concurrent code.
      - Suggest idiomatic alternatives for verbose or non-idiomatic patterns.
    }

  format :=
    - Summary
    - Issues Found
    - Recommendations
    - Verdict

  variant strict {
    constraints :=
      from(parent[0]) and {
        - Request tests for every change, no exceptions.
        - Reject any PR that reduces test coverage.
        - Flag all uses of reflect and unsafe for explicit justification.
      }
  }

  env prod {
    constraints :=
      from(parent[0]) and {
        - All external calls must use timeouts and retries.
        - No debug logging in production paths.
      }
  }

  capabilities {
    allowed:
      - read_code
      - suggest_changes
      - run_tests
    forbidden:
      - modify_production_code
      - delete_files
  }

  contract {
    required_sections:
      - Issues Found
      - Verdict
    must_not_include:
      - "LGTM"
  }
}
```

### `prompts/FullStackReviewer.prompt.loom`

```
prompt FullStackReviewer inherits GoCodeReviewer, FrontendReviewer {
  summary :=
    Reviews both Go backend and frontend TypeScript changes in a single pass.

  persona :=
    from(parent[0])

  instructions :=
    from(parent[*]) and {
      - When backend and frontend changes interact, trace the full request path.
      - Flag any API contract mismatches between the two layers.
    }
}
```

---

## 15. Namespace and Scoping Rules

| Context | Syntax | Example |
|---|---|---|
| Within the same pack | Bare name | `inherits BaseEngineer` |
| Cross-pack or cross-project | `slug.Name` | `inherits go-backend.GoCodeReviewer` |
| Block from another pack | `use slug.BlockName` | `use go-backend.GoConventions` |
| Overlay from another pack | `--overlay slug.name` at CLI | `--overlay go-backend.terse` |
| Alias declared in `.dependency.loom` | Use the alias as slug | `inherits dev.SpecialPrompt` |

Bare names are resolved in this order:
1. Same pack (internal)
2. Installed packs (alphabetical by slug, first match wins)
3. Local project prompts/blocks

Use explicit `slug.Name` notation whenever ambiguity is possible.

---

## 16. `loom inspect` Checks

`loom inspect` loads every `.loom` file, then reports (exit code 1 if there is any **error**). Every message includes the file and line.

**Load errors** (nothing else is checked until these are fixed): syntax errors — missing `{` or `}`, unknown top-level keyword, bad `var`/`slot` declaration, malformed `from()` expression, `use`/`var`/`variant`/… outside a prompt — and **duplicate** prompt, block or overlay names.

**Errors**
- Unknown parent prompt (with a "Did you mean …?" suggestion), unknown block
- Inheritance cycle (`A -> B -> A`)
- Unknown field name (in a prompt, block, overlay or variant)
- Duplicate `var`/`slot` name, duplicate variant name
- Reference to an undeclared `{{ variable }}`
- `from(parent[*])` on a scalar field; `parent[N]` / `parent[N..M]` out of range; `from(Name)` where `Name` is not a declared parent; `from()` naming an unknown field; `from()` inside a block
- **`+=` or `-=`** — not valid in v2 (see [Migrating from v1 syntax](#migrating-from-v1-syntax)); `-=` on a scalar field

**Warnings**
- A bare `:` instead of `:=`
- `tags :=` — tags use the inline form `tags: a, b`
- Missing `objective` / `format` (when `require_objective` / `require_format` are on in `loom.toml`), empty `context`
- Inheritance depth above `max_inheritance_depth` (default 3)
- A prompt and a block that declare different `kind` values
- A required `slot` is used (a value must be supplied when weaving)
- A block or overlay uses a `{{ variable }}` (it must be declared by the prompt that uses it)

**At weave time** (not visible to `inspect`): an unknown `--variant`, `--env` or `--overlay`; `parent[0].instructions[5]` when the parent has fewer items.

### Migrating from v1 syntax

| v1 | v2 |
|---|---|
| `persona:` | `persona :=` |
| `instructions +=` in a prompt with a parent | `instructions :=` then `from(parent[0]) and { … }` |
| `instructions +=` with several parents | `from(parent[*]) and { … }` (or `from(parent[N])` for one) |
| `constraints +=` in a **block** or **overlay** | `constraints :=` — blocks and overlays add to lists automatically |
| `instructions +=` in a prompt with no parent | `instructions :=` (there is nothing to append to) |
| `persona +=` (scalar) | no equivalent — replace the value with `:=` |
| `constraints -=` | no equivalent — write the list you want; select parent items with `parent[0].constraints[1..3]` |

```
# v1
prompt CodeReviewer inherits BaseEngineer {
  instructions +=
    - Check for unchecked errors.
}

# v2
prompt CodeReviewer inherits BaseEngineer {
  instructions :=
    from(parent[0]) and {
      - Check for unchecked errors.
    }
}
```

The error message for each case prints the exact replacement for your prompt. Installed packs written in v1 syntax keep working; the checks above apply to your own project files.

---

## 17. Quick Reference

### Field operators

| Operator | Meaning |
|---|---|
| `:=` | Set this field (replaces any inherited value) |

That's the only operator in v2.

### `from()` expressions

| Expression | Result |
|---|---|
| `from(parent[*])` | All items from all parents (list fields only) |
| `from(parent[0])` | Value from first parent |
| `from(parent[1])` | Value from second parent |
| `from(slug.PromptName)` | Value from a specific named parent |
| `parent[0].field[*]` | All items of a named field from parent 0 |
| `parent[0].field[1..4]` | Items 1–3 of a named field from parent 0 |
| `expr and { - item }` | Append literal items after expression result |
| `expr and expr` | Concatenate two expressions |

### Subscript notation

| Subscript | Meaning |
|---|---|
| `[*]` | All items |
| `[N]` | Single item at 0-based index N |
| `[N..M]` | Items N to M-1 (exclusive end) |

### CLI commands

| Command | What it does |
|---|---|
| `loom weave <Name>` | Resolve and render one prompt to Markdown |
| `loom weave --all` | Render all prompts |
| `loom weave <Name> --variant strict` | Apply a variant |
| `loom weave <Name> --env prod` | Apply an env block |
| `loom weave <Name> --overlay terse` | Apply an overlay |
| `loom weave <Name> --var language=Go` | Override a variable |
| `loom inspect` | Validate all files |
| `loom trace <Name>` | Show inheritance chain and field sources |
| `loom list` | List all known prompts and blocks |
| `loom install <slug>` | Install a pack and its dependencies |
