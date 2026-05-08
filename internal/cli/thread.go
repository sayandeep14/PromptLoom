package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var threadCmd = &cobra.Command{
	Use:   "thread",
	Short: "Scaffold a new .loom source file",
}

var threadPromptInherits string

var threadPromptCmd = &cobra.Command{
	Use:   "prompt <Name>",
	Short: "Create a new .prompt.loom file",
	Args:  cobra.ExactArgs(1),
	RunE:  runThreadPrompt,
}

var threadBlockCmd = &cobra.Command{
	Use:   "block <Name>",
	Short: "Create a new .block.loom file",
	Args:  cobra.ExactArgs(1),
	RunE:  runThreadBlock,
}

var threadOverlayCmd = &cobra.Command{
	Use:   "overlay <Name>",
	Short: "Create a new .overlay.loom file",
	Args:  cobra.ExactArgs(1),
	RunE:  runThreadOverlay,
}

var threadVarsCmd = &cobra.Command{
	Use:   "vars <Name>",
	Short: "Create a new .vars.loom file",
	Args:  cobra.ExactArgs(1),
	RunE:  runThreadVars,
}

func init() {
	threadPromptCmd.Flags().StringVar(&threadPromptInherits, "inherits", "", "parent prompt name")
	threadCmd.AddCommand(threadPromptCmd)
	threadCmd.AddCommand(threadBlockCmd)
	threadCmd.AddCommand(threadOverlayCmd)
	threadCmd.AddCommand(threadVarsCmd)
}

func runThreadPrompt(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return err
	}

	filename := toKebabCase(name) + ".prompt.loom"
	dest := filepath.Join(cwd, cfg.Paths.Prompts, filename)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("file already exists: %s", dest)
	}

	var sb strings.Builder
	if threadPromptInherits != "" {
		fmt.Fprintf(&sb, "prompt %s inherits %s {\n", name, threadPromptInherits)
	} else {
		fmt.Fprintf(&sb, "prompt %s {\n", name)
	}
	sb.WriteString("  summary:\n    \n\n")
	sb.WriteString("  persona:\n    \n\n")
	sb.WriteString("  objective:\n    \n\n")
	sb.WriteString("  constraints:\n    - \n\n")
	sb.WriteString("  format:\n    - \n}\n")

	return writeScaffold(dest, sb.String())
}

func runThreadBlock(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return err
	}

	filename := toKebabCase(name) + ".block.loom"
	dest := filepath.Join(cwd, cfg.Paths.Blocks, filename)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("file already exists: %s", dest)
	}

	content := fmt.Sprintf("block %s {\n  constraints:\n    - \n}\n", name)
	return writeScaffold(dest, content)
}

func runThreadOverlay(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		return err
	}

	filename := toKebabCase(name) + ".overlay.loom"
	dest := filepath.Join(cwd, cfg.Paths.Overlays, filename)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("file already exists: %s", dest)
	}

	content := fmt.Sprintf("overlay %s {\n  // Add field overrides here.\n  // instructions +=\n  //   - Additional instruction\n}\n", name)
	return writeScaffold(dest, content)
}

func runThreadVars(cmd *cobra.Command, args []string) error {
	name := args[0]
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	filename := toKebabCase(name) + ".vars.loom"
	dest := filepath.Join(cwd, filename)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("file already exists: %s", dest)
	}

	content := `// Project-level variable defaults.
// These values apply to all prompts unless overridden per-prompt or via --set.

var model = "gpt-4"
var temperature = "0.7"

// slot example {}
// slot task { required: true }
`
	return writeScaffold(dest, content)
}

func writeScaffold(dest, content string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
		return fmt.Errorf("could not write %s: %w", dest, err)
	}
	fmt.Printf("  %s  created  %s\n", tui.SuccessStyle.Render("✓"), tui.PathStyle.Render(dest))
	return nil
}

// toKebabCase converts CamelCase to kebab-case.
// "SpringBootReviewer" → "spring-boot-reviewer"
func toKebabCase(name string) string {
	runes := []rune(name)
	var sb strings.Builder
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
				nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if prevLower || nextLower {
					sb.WriteByte('-')
				}
			}
			sb.WriteRune(r + 32)
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
