# Changelog

All notable changes to Lumine are documented here.

---

## [0.2.0] — 2026-09-21

Lumine now speaks **v2 only**: `:=` is the one field operator, matching `loom inspect`.

### Added
- **Quick fixes** (Ctrl/Cmd+.) and a *Fix all v1 syntax* code action: bare `:` → `:=`, `extends` → `inherits`, `+=` in a child prompt → `:= from(parent[0]) and { … }`, `+=` in blocks/overlays/parentless prompts → `:=`
- `env` blocks are parsed (their fields were previously mis-attributed to the prompt) and appear in the Outline
- `env` snippet; `env` bodies get field completions
- Test suite (`npm test`) including a parity check against the Go fixtures in `testdata/`, and `npm run typecheck`

### Changed
- `+=` and `-=` are now **errors** whose message prints the exact v2 rewrite (previously "deprecated" warnings); `extends` is an error; a bare `:` is a warning with the replacement
- The checks also cover `block`, `overlay`, `variant` and `env` bodies; `contract`/`capabilities` keys keep their colon and are never flagged
- Inherited-field detection now finds parents defined in the **same file** and in ancestors, not just direct parents in other files
- Completions offer only `:=` (and `key:` inside `contract`/`capabilities`); hover documents `:=` and gives migration advice for legacy operators
- Grammar highlights legacy operators as deprecated; snippets emit v2 syntax (the overlay snippet no longer suggests `from(parent[*])`, which is invalid in overlays)
- Scalar `-=` message now matches the CLI

### Fixed
- **Format Document deleted content.** It rebuilt the file from the parse tree, dropping comments, `env` blocks, `tags`, and every parent after the first in `inherits A, B`. It now only normalises whitespace (and no longer reorders body elements)
- `tsc` type errors in the parser and TOML config reader

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
