package cli

import (
	"os"

	"github.com/sayandeepgiri/promptloom/internal/lsp"
	"github.com/spf13/cobra"
)

var lspCmd = &cobra.Command{
	Use:   "lsp",
	Short: "Start the Language Server Protocol server",
	Long: `Start a Language Server Protocol (LSP) server on stdin/stdout.

Used by editor integrations (Neovim, Emacs, etc.) to provide:
  - Real-time diagnostics from loom inspect
  - Hover documentation for fields, operators, and prompt references
  - Go-to-definition for inherits and use references
  - Completions for field names, block names, and prompt names
  - Document symbols (outline panel)

The server communicates using JSON-RPC 2.0 with Content-Length framing.
It must not be started manually — editors launch it automatically.

Neovim (nvim-lspconfig):
  require('lspconfig').promptloom.setup({
    cmd = { "loom", "lsp" },
    filetypes = { "promptloom" },
    root_dir = require('lspconfig.util').root_pattern("loom.toml"),
  })`,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := lsp.New(os.Stdin, os.Stdout)
		return srv.Run()
	},
	// Silence usage on error — LSP errors go nowhere useful.
	SilenceUsage: true,
}

func init() {
	// No flags; the LSP server takes no CLI arguments.
}
