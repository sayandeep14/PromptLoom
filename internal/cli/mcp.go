package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/internal/loader"
	"github.com/sayandeepgiri/promptloom/internal/mcp"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP tool manifest generation",
	Long:  `Commands for generating MCP-compatible tool manifests from PromptLoom prompts.`,
}

var mcpManifestAll bool
var mcpManifestOut string

var mcpManifestCmd = &cobra.Command{
	Use:   "manifest [Name]",
	Short: "Generate an MCP tool manifest",
	Long: `Generate an MCP-compatible tool manifest from one or all prompts.

Reads contract {} and capabilities {} blocks to produce a structured
tool definition compatible with the Model Context Protocol.

Examples:
  loom mcp manifest
  loom mcp manifest SecurityReviewer
  loom mcp manifest --all --out .claude/mcp-prompts.json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMCPManifest,
}

func init() {
	mcpManifestCmd.Flags().BoolVar(&mcpManifestAll, "all", false, "generate manifest for all prompts")
	mcpManifestCmd.Flags().StringVar(&mcpManifestOut, "out", "", "write manifest JSON to this file path")
	mcpCmd.AddCommand(mcpManifestCmd)
}

func runMCPManifest(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !mcpManifestAll {
		mcpManifestAll = true
	}
	if mcpManifestAll && len(args) > 0 {
		return fmt.Errorf("cannot combine a prompt name with --all")
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return fmt.Errorf("failed to load project: %w", err)
	}

	var manifest *mcp.Manifest
	var warnings []string

	if mcpManifestAll {
		manifest, warnings, err = mcp.GenerateAll(reg)
	} else {
		manifest, warnings, err = mcp.Generate(args[0], reg)
	}
	if err != nil {
		return err
	}
	if manifest == nil {
		return fmt.Errorf("prompt %q not found", args[0])
	}

	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, tui.WarningStyle.Render("  warn: "+w))
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	if mcpManifestOut != "" {
		outPath := mcpManifestOut
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(cwd, outPath)
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
		fmt.Println(tui.SuccessStyle.Render("  wrote ") + tui.PathStyle.Render(outPath))
		return nil
	}

	fmt.Println(string(data))
	return nil
}
