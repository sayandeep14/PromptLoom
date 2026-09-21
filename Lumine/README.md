# Lumine — PromptLoom DSL for VS Code

Full IDE support for the [Loom](https://github.com/sayandeep14/PromptLoom) prompt-engineering DSL — the tool that treats AI prompts as source code.

---

## Features

### Syntax Highlighting

Semantic color coding for all Loom constructs: `prompt`, `block`, `overlay` declarations, the field operator `:=` (legacy `+=`, `-=` and a bare `:` are highlighted as deprecated), `{{ variable }}` tokens, list bullets, and sub-blocks (`variant`, `contract`, `capabilities`).

### IntelliSense Completions

- **Top-level keywords** — `prompt`, `block`, `overlay` with snippet bodies
- **Body keywords** — `use`, `var`, `slot`, `variant`, `env`, `contract`, `capabilities`
- **All field names** with the one v2 operator (`summary :=`, `instructions :=`, …); `contract` / `capabilities` keys complete as `key:`
- **Cross-file prompt names** after `inherits`
- **Cross-file block names** after `use`
- **Variable names** inside `{{ }}` tokens — local and global
- **`loom.toml` keys** — per-section completions with types and defaults

### Hover Documentation

Hover over any token to see:
- Field descriptions
- The `:=` operator, and migration advice when you hover a legacy `+=`, `-=` or `:`
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
| Invalid field name | `typo :=` |
| **`+=` or `-=`** (v1 syntax, removed in v2) | `instructions +=` — the message prints the exact v2 rewrite |
| `-=` on scalar field | `summary -=` |
| **`extends`** (v1 keyword) | `prompt B extends A` — use `inherits` |
| Duplicate var/slot name | Declaring the same name twice |
| Duplicate prompt/block name | Same name in multiple files |
| Undefined `{{ variable }}` | Token with no matching declaration |

The same rules apply inside `block`, `overlay`, `variant` and `env` bodies. `contract` and `capabilities` keys (`required_sections:`, `allowed:`, …) legitimately use a colon and are never flagged. Messages match `loom inspect` word for word.

**Warnings** (yellow squiggles, configurable via `loom.toml`):
| Check | Config key |
|---|---|
| Missing `objective` field | `require_objective = true` |
| Missing `format` field | `require_format = true` |
| Missing `contract` block | `require_contract = true` |
| Empty `context` field | `warn_on_empty_context = true` |
| Inheritance chain too deep | `warn_on_deep_inheritance = true` |
| **Bare `:` instead of `:=`** | `persona:` — use `persona :=` |
| Block uses undeclared variable | always |
| Slot with no required/default | always |

### Navigation

- **Go to Definition** (`F12` / Ctrl+click) — jump to prompt declarations, block declarations, and variable declarations across all files
- **Find All References** (`Shift+F12`) — find every `inherits`, `use`, and `{{ }}` reference across the workspace
- **Document Symbols** — Outline panel shows all prompts, blocks, overlays, vars, fields, and variants in the current file

### Quick Fixes (v1 → v2)

Press **Ctrl/Cmd+.** on a v1-syntax diagnostic:

| Diagnostic | Fix |
|---|---|
| `persona:` | `persona :=` |
| `prompt B extends A` | `prompt B inherits A` |
| `instructions +=` in a child prompt | `instructions :=` + `from(parent[0]) and { … }` (or `parent[*]` with several parents) |
| `constraints +=` in a block/overlay, or in a prompt without a parent | `constraints :=` |

**Fix all v1 syntax in this file** applies every fix at once (also available as the `source.fixAll` code action).

No automatic fix is offered where the meaning cannot be preserved — `-=`, `+=` on a scalar field, `+=` on `format`, and `+=` inside `variant`/`env` blocks. The diagnostic's message explains the manual rewrite.

### Auto-Formatter

**Format Document** (`Shift+Alt+F`) normalises whitespace only, so it can never lose content:
- trailing whitespace removed, tabs become two spaces, blank lines collapsed
- operator spacing: `persona:=` → `persona :=`
- comments, `env` blocks, `tags`, all parents of `inherits A, B`, and the order of your body elements are left exactly as written
- Enable **Format on Save** via `loom.formatOnSave`

> The formatter no longer reorders body elements. Earlier versions rebuilt the file from the parse tree, which deleted comments, `env` blocks and every parent after the first.

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

## Installation

**From a downloaded `.vsix`** (no Marketplace account needed):

1. Download [`lumine-latest.vsix`](https://github.com/sayandeep14/PromptLoom/releases/download/lumine-latest/lumine-latest.vsix) (or `lumine-<version>.vsix` from a [versioned release](https://github.com/sayandeep14/PromptLoom/releases?q=lumine-v)).
2. Install it, either way:
   - **VS Code UI:** open the Extensions view (`Ctrl/Cmd+Shift+X`) → the `…` menu at the top → **Install from VSIX…** → pick the file.
   - **Terminal:** `code --install-extension lumine-latest.vsix`
3. Reload the window. Open any `.prompt.loom`, `.block.loom` or `.overlay.loom` file.

To update, install the newer `.vsix` the same way (it replaces the old one). To remove it: Extensions view → Lumine → **Uninstall**, or `code --uninstall-extension shreekalpo.lumine`.

**Build it yourself:** `npm install && npx vsce package --no-dependencies` in this folder produces the `.vsix`.

---

## Requirements

- VS Code 1.85 or later
- [Loom CLI](https://github.com/sayandeep14/PromptLoom) installed and on `PATH` for CLI commands (the language server features work without it)

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

## Development

```bash
npm install
npm run build        # bundle the extension and the language server
npm run typecheck    # tsc over src/ and test/
npm test             # unit tests (Node's built-in runner)
```

The tests also run the extension over the fixtures in the repository's `testdata/` directory — the same ones the Go CLI is tested with — so the editor and `loom inspect` cannot drift apart. If a Go toolchain is available, one test builds `loom` and checks that quick-fixed output passes `loom inspect`.

---

## License

MIT
