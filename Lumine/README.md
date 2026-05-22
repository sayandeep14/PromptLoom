# Lumine — PromptLoom DSL for VS Code

Full IDE support for the [Loom](https://github.com/sayandeepgiri/loom) prompt-engineering DSL — the tool that treats AI prompts as source code.

---

## Features

### Syntax Highlighting

Semantic color coding for all Loom constructs: `prompt`, `block`, `overlay` declarations, field operators (`:` `:=` `+=` `-=`), `{{ variable }}` tokens, list bullets, and sub-blocks (`variant`, `contract`, `capabilities`).

### IntelliSense Completions

- **Top-level keywords** — `prompt`, `block`, `overlay` with snippet bodies
- **Body keywords** — `use`, `var`, `slot`, `variant`, `contract`, `capabilities`
- **All field names** with each valid operator (`summary:`, `instructions+=`, etc.)
- **Cross-file prompt names** after `inherits`
- **Cross-file block names** after `use`
- **Variable names** inside `{{ }}` tokens — local and global
- **`loom.toml` keys** — per-section completions with types and defaults

### Hover Documentation

Hover over any token to see:
- Field descriptions and which operators are valid
- Operator semantics (define / override / append / remove)
- Prompt details: parent, fields defined, which prompts inherit it
- Block details: fields, which prompts use it
- Variable details: type (var/slot), default value, required status
- `loom.toml` keys: type, default, and description

### Real-Time Diagnostics

**Errors** (red squiggles):
| Check | Trigger |
|---|---|
| Unknown parent prompt | `inherits NonExistent` |
| Unknown block reference | `use NonExistent` |
| Inheritance cycle | `A → B → A` |
| Invalid field name | `typo:` |
| `-=` on scalar field | `summary -=` |
| Duplicate var/slot name | Declaring the same name twice |
| Duplicate prompt/block name | Same name in multiple files |
| Undefined `{{ variable }}` | Token with no matching declaration |

**Warnings** (yellow squiggles, configurable via `loom.toml`):
| Check | Config key |
|---|---|
| Missing `objective` field | `require_objective = true` |
| Missing `format` field | `require_format = true` |
| Missing `contract` block | `require_contract = true` |
| Empty `context` field | `warn_on_empty_context = true` |
| Inheritance chain too deep | `warn_on_deep_inheritance = true` |
| Ambiguous `:` on inherited field | always |
| Block uses undeclared variable | always |
| Slot with no required/default | always |

### Navigation

- **Go to Definition** (`F12` / Ctrl+click) — jump to prompt declarations, block declarations, and variable declarations across all files
- **Find All References** (`Shift+F12`) — find every `inherits`, `use`, and `{{ }}` reference across the workspace
- **Document Symbols** — Outline panel shows all prompts, blocks, overlays, vars, fields, and variants in the current file

### Auto-Formatter

**Format Document** (`Shift+Alt+F`) produces canonical Loom output matching `loom fmt`:
- Body elements in canonical order: vars → use → fields → variants → contract → capabilities
- Consistent 2-space / 4-space indentation
- Blank lines between element groups, no trailing blank before `}`
- Enable **Format on Save** via `loom.formatOnSave`

### CLI Commands

| Command | Palette entry | What it does |
|---|---|---|
| `loom.weave` | **Loom: Weave (Render) This Prompt** | Runs `loom weave <current-file>` in the integrated terminal |
| `loom.inspect` | **Loom: Inspect (Validate) Library** | Runs `loom inspect` in the integrated terminal |
| `loom.openGraph` | **Loom: Open Dependency Graph** | Runs `loom graph` in the integrated terminal |

The **Weave** button also appears in the editor title bar when a `.loom` file is open.

### File Icons

Distinct icons in the File Explorer for each Loom file type (requires activating the **Lumine File Icons** theme via *File > Preferences > File Icon Theme*):

| Icon | File type |
|---|---|
| Blue `P` | `.prompt.loom` |
| Orange `B` | `.block.loom` |
| Purple `O` | `.overlay.loom` |
| Green `V` | `.vars.loom` |
| Grey `L` | `.loom` (mixed) |
| Red gear | `loom.toml` |

### Code Snippets

| Prefix | Inserts |
|---|---|
| `prompt` | Full prompt declaration skeleton |
| `prompti` | Prompt with inheritance |
| `block` | Block declaration |
| `overlay` | Overlay declaration |
| `use` | `use BlockName` |
| `var` | Variable declaration |
| `slot` | Slot declaration |
| `variant` | Variant block |
| `contract` | Contract block |
| `capabilities` | Capabilities block |
| `{{` | Variable interpolation token |

---

## Requirements

- VS Code 1.85 or later
- [Loom CLI](https://github.com/sayandeepgiri/loom) installed and on `PATH` for CLI commands (the language server features work without it)

---

## Extension Settings

| Setting | Default | Description |
|---|---|---|
| `loom.loomExecutable` | `"loom"` | Path to the loom CLI binary |
| `loom.validateOnSave` | `true` | Run validation when a file is saved |
| `loom.formatOnSave` | `false` | Auto-format `.loom` files on save |
| `loom.trace.server` | `"off"` | LSP trace level (`off` / `messages` / `verbose`) |

---

## `loom.toml` Support

Lumine provides completions and hover documentation when editing `loom.toml`. Type `[` to get section header completions, then start a new line inside a section to see all available keys with their types, defaults, and descriptions.

---

## License

MIT
