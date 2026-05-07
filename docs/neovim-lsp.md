# PromptLoom LSP — Neovim Setup

`loom lsp` is a Language Server Protocol server built into the `loom` binary. It provides diagnostics, hover docs, go-to-definition, completions, and document symbols for `.loom` files.

---

## Prerequisites

- `loom` installed and on `$PATH` (`go install github.com/sayandeepgiri/promptloom/cmd/loom@latest` or a local build)
- [nvim-lspconfig](https://github.com/neovim/nvim-lspconfig) installed

---

## Option A — nvim-lspconfig (recommended)

`loom lsp` is not yet in the upstream `nvim-lspconfig` registry, so register it manually:

```lua
local lspconfig = require('lspconfig')
local configs = require('lspconfig.configs')

-- Register the server if not already registered.
if not configs.promptloom then
  configs.promptloom = {
    default_config = {
      cmd = { 'loom', 'lsp' },
      filetypes = { 'promptloom' },
      root_dir = lspconfig.util.root_pattern('loom.toml'),
      settings = {},
    },
  }
end

lspconfig.promptloom.setup({})
```

Then associate the filetype in your `init.lua` or `ftdetect/`:

```lua
vim.filetype.add({
  extension = {
    loom = 'promptloom',
  },
})
```

---

## Option B — vim.lsp.start (no plugin required, Neovim 0.8+)

```lua
vim.api.nvim_create_autocmd('FileType', {
  pattern = 'promptloom',
  callback = function()
    vim.lsp.start({
      name = 'loom-lsp',
      cmd = { 'loom', 'lsp' },
      root_dir = vim.fs.dirname(
        vim.fs.find({ 'loom.toml' }, { upward = true })[1]
      ),
    })
  end,
})

vim.filetype.add({ extension = { loom = 'promptloom' } })
```

---

## Features

| Feature | Trigger |
|---|---|
| **Diagnostics** | Automatic on open and save — mirrors `loom inspect` output |
| **Hover** | `K` — shows field documentation or resolved prompt info |
| **Go to definition** | `gd` — jumps from `inherits Name` / `use Name` to the declaration |
| **Completions** | `<C-Space>` — field names, operators, block/prompt names |
| **Document symbols** | `:Telescope lsp_document_symbols` or equivalent |

---

## Keymaps (example)

```lua
vim.api.nvim_create_autocmd('LspAttach', {
  callback = function(args)
    local opts = { buffer = args.buf }
    vim.keymap.set('n', 'gd',         vim.lsp.buf.definition,     opts)
    vim.keymap.set('n', 'K',          vim.lsp.buf.hover,           opts)
    vim.keymap.set('n', '<leader>ca', vim.lsp.buf.code_action,     opts)
    vim.keymap.set('n', '[d',         vim.diagnostic.goto_prev,    opts)
    vim.keymap.set('n', ']d',         vim.diagnostic.goto_next,    opts)
  end,
})
```

---

## Troubleshooting

**Server does not start:**
- Run `loom --version` to verify the binary is on `$PATH`.
- Check `:LspLog` for startup errors.

**No diagnostics:**
- Make sure `loom.toml` exists in the project root (the server uses it to locate the project).
- Run `loom inspect` in the terminal to confirm the project loads cleanly.

**Wrong filetype:**
- Verify `:set filetype?` shows `promptloom` when editing a `.loom` file.
- Add the `filetype.add` snippet shown above if not.
