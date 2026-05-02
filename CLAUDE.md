# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Status

PromptLoom is currently in the **specification phase**. The full product spec lives in `prompt_tool_v_1_spec.md`. There is no implementation code yet. When building, refer to that document as the authoritative source of truth.

## What This Tool Does

PromptLoom (`loom` is the CLI binary name) is a developer-first CLI that treats prompts like source code — with inheritance, block composition, validation, and Markdown rendering. Prompts are stored in `.prompt` DSL files; the tool parses, validates, resolves, and renders them.

## Intended Tech Stack

- **Language:** Go (recommended in spec §14), with standard CLI structure under `cmd/loom/`
- **CLI framework:** Cobra (expected, given Go + subcommand design)
- **Config format:** TOML (`loom.toml` in project root)
- **Output:** Markdown files rendered from resolved prompts

## Intended Project Layout (from spec §14)

```
promptloom/
  cmd/loom/main.go
  internal/
    ast/        # AST node types
    config/     # loom.toml loading
    lexer/      # DSL tokenizer
    parser/     # builds AST from tokens
    registry/   # registers prompts/blocks, detects duplicates
    validate/   # syntax, ref, cycle, and field checks
    resolve/    # inheritance resolution, block composition, field ops
    render/     # converts resolved model → Markdown
    cli/        # subcommand definitions (cobra handlers)
  testdata/
    valid/
    invalid/
  go.mod
```

## Build & Test Commands (to be wired up in Milestone 1)

```bash
go build ./cmd/loom               # build binary
go test ./...                     # run all tests
go test ./internal/parser/...     # run a single package's tests
go vet ./...                      # lint
```

## Processing Pipeline

```
.prompt files → Lexer → Parser → AST → Registry → Validator
  → Resolver (inheritance + blocks + field ops) → Renderer → Markdown output
```

## Key DSL Concepts

- **Prompt fields:** `name`, `summary`, `persona`, `context`, `objective`, `instructions`, `constraints`, `examples`, `format`, `notes`
- **Field operators:** `:=` (override), `+=` (append), `-=` (remove)
- **Inheritance:** `extends ParentPromptName`
- **Block mixin:** `use BlockName`
- **Blocks** are reusable instruction sets, not full prompts

## CLI Commands (from spec §11)

| Command | Purpose |
|---|---|
| `loom init` | Initialize new project with `loom.toml` |
| `loom list` | List all prompts and blocks |
| `loom validate` | Validate entire library (errors + warnings) |
| `loom weave <Name>` | Render single prompt to Markdown |
| `loom weave --all` | Render all prompts |
| `loom explain <Name>` | Show inheritance chain and resolution trace |
| `loom expand <Name>` | Show fully expanded prompt pre-render |
| `loom new prompt <Name>` | Scaffold a new prompt file |
| `loom new block <Name>` | Scaffold a new block file |
| `loom fmt` | Format `.prompt` files (optional for V1) |

## Development Milestones (spec §20)

1. CLI skeleton (init, new, list stubs)
2. Lexer + Parser + AST
3. Registry + Validator
4. Resolver (inheritance, blocks, field ops)
5. Markdown renderer + render command
6. Explain + expand commands
7. Polish, testdata, examples, README

## Validation Rules (spec §15)

Errors (hard fail): undefined references, inheritance cycles, duplicate names, unknown field names, invalid operator usage, missing required fields on `extends` prompts.

Warnings (soft): deep inheritance chains (>3 levels), overriding `+=` with `:=`, unused blocks.

## Error Message Philosophy (spec §21)

Errors must include: what went wrong, which file and line, what to do to fix it. Modeled after Rust-style compiler errors — never just "invalid syntax".
