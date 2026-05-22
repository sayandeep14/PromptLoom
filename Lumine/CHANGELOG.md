# Changelog

All notable changes to Lumine are documented here.

---

## [0.1.0] — 2026-05-05

Initial release with full IDE support for the Loom DSL.

### Added

**Core language support**
- TextMate grammar with 12 patterns covering all Loom syntax elements
- Language configuration: `//` comments, `{}` bracket matching, `{{ }}` auto-close, word pattern
- 11 code snippets for rapid authoring (`prompt`, `prompti`, `block`, `overlay`, `use`, `var`, `slot`, `variant`, `contract`, `capabilities`, `{{`)

**Language server features**
- Real-time error diagnostics: unknown parents/blocks, inheritance cycles, invalid fields, `-=` on scalars, duplicate names, undefined `{{ variables }}`
- Configurable warning diagnostics via `loom.toml`: missing objective/format/contract, empty context, deep inheritance, ambiguous `:`, undeclared block variables, slots with no required/default
- IntelliSense completions: top-level keywords, body keywords, all field+operator combinations, cross-file prompt/block names, variable tokens, `loom.toml` keys
- Hover documentation for every token type: fields, operators, prompts, blocks, variables, `loom.toml` keys
- Go to Definition (`F12`): prompt declarations, block declarations, variable declarations
- Find All References (`Shift+F12`): all `inherits`, `use`, and `{{ }}` occurrences across the workspace
- Document Symbols: full outline tree with prompts, blocks, overlays, fields, variants, vars
- Auto-formatter producing canonical `loom fmt` output with proper ordering, indentation, and blank lines

**Workspace integration**
- Automatic registry seeding from all `.loom` files on workspace open
- Cross-file duplicate detection and cross-file reference resolution
- `loom.toml` watcher — warning diagnostics update automatically when config changes
- Format on save (`loom.formatOnSave`)

**CLI integration**
- **Loom: Weave** — renders the active prompt file via `loom weave`; appears in editor title bar
- **Loom: Inspect** — validates the full library via `loom inspect`
- **Loom: Open Dependency Graph** — runs `loom graph`
- Commands reuse a single `Loom` terminal session

**File icons** (requires activating Lumine File Icons theme)
- Distinct icons for `.prompt.loom`, `.block.loom`, `.overlay.loom`, `.vars.loom`, `.loom`, and `loom.toml`
