package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/importer"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var importName string
var importOut string
var importDir string
var importForce bool

var importCmd = &cobra.Command{
	Use:   "import [file.md]",
	Short: "Convert a Markdown prompt to PromptLoom DSL",
	Long: `Heuristic Markdown parser that converts well-structured prompt Markdown files
into PromptLoom .loom DSL files.

Reads ## headings as field names (Persona, Instructions, Constraints, etc.),
bullet lists as list field values, and paragraphs as scalar values.

Examples:
  loom import old-prompts/CodeReviewer.md
  loom import old-prompts/CodeReviewer.md --name CodeReviewer --out prompts/
  loom import --dir old-prompts/ --out prompts/`,
	Args: cobra.MaximumNArgs(1),
	RunE: runImport,
}

func init() {
	importCmd.Flags().StringVar(&importName, "name", "", "prompt name to use (default: derived from filename)")
	importCmd.Flags().StringVar(&importOut, "out", "", "output directory (default: prompts/)")
	importCmd.Flags().StringVar(&importDir, "dir", "", "import all .md files from this directory")
	importCmd.Flags().BoolVar(&importForce, "force", false, "overwrite existing .loom files")
}

func runImport(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && importDir == "" {
		return fmt.Errorf("specify a Markdown file or use --dir <directory>")
	}
	if len(args) > 0 && importDir != "" {
		return fmt.Errorf("cannot combine a file argument with --dir")
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	outDir := importOut
	if outDir == "" {
		outDir = filepath.Join(cwd, "prompts")
	} else if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(cwd, outDir)
	}

	var files []string
	if importDir != "" {
		dir := importDir
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(cwd, dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read dir %s: %w", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			lower := strings.ToLower(e.Name())
			if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
		if len(files) == 0 {
			return fmt.Errorf("no Markdown files found in %s", importDir)
		}
	} else {
		path := args[0]
		if !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}
		files = []string{path}
	}

	var written, skipped int
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render(fmt.Sprintf("  error reading %s: %v", path, err)))
			continue
		}

		name := importName
		if name == "" || len(files) > 1 {
			name = importer.NameFromPath(path)
		}

		result := importer.Import(string(src), name)

		for _, w := range result.Warnings {
			fmt.Println(tui.WarningStyle.Render("  warn ["+name+"]: "+w))
		}

		outPath := filepath.Join(outDir, name+".prompt.loom")
		if _, err := os.Stat(outPath); err == nil && !importForce {
			fmt.Println(tui.MutedStyle.Render("  skip ") + tui.PathStyle.Render(outPath) + tui.MutedStyle.Render(" (already exists; use --force to overwrite)"))
			skipped++
			continue
		}

		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
		if err := os.WriteFile(outPath, []byte(result.DSL), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		fmt.Println(tui.SuccessStyle.Render("  wrote ") + tui.PathStyle.Render(outPath))
		written++
	}

	fmt.Println()
	fmt.Printf("  %d written, %d skipped\n", written, skipped)

	// Run inspect automatically after import to surface any issues.
	if written > 0 {
		fmt.Println()
		out, hasErr := tui.RunInspect(cwd)
		fmt.Print(out)
		if hasErr {
			fmt.Println(tui.WarningStyle.Render("  Run `loom inspect` for details."))
		}
	}

	return nil
}
