package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/installer"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var installRegistry string

var installCmd = &cobra.Command{
	Use:   "install <vault-name>",
	Short: "Download and compile a prompt-pack from the registry",
	Long: `Fetch a named vault from the PromptLoom registry and install it under
loompack/<vault-name>/ in the current directory.

Source .loom files are written to  loompack/<vault-name>/source/
Compiled .md files are written to   loompack/<vault-name>/compiled/
Pack metadata is saved as           loompack/<vault-name>/pack.json

Registry URL resolution order:
  1. --registry flag
  2. $LOOM_REGISTRY_URL environment variable
  3. LOOM_REGISTRY_URL in loom/.loom.env
  4. Default: https://registry.promptloom.dev

Examples:
  loom install go-backend
  loom install python-data-science`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

func init() {
	installCmd.Flags().StringVar(&installRegistry, "registry", "",
		"registry base URL (overrides $LOOM_REGISTRY_URL and .loom.env)")
}

func runInstall(cmd *cobra.Command, args []string) error {
	vaultName := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	registryURL := resolveRegistryURL(cwd)
	if installRegistry != "" {
		registryURL = installRegistry
	}
	// Propagate so installer.Install picks it up via os.Getenv.
	os.Setenv("LOOM_REGISTRY_URL", registryURL)

	fmt.Printf("%s  fetching %s from %s…\n",
		tui.MutedStyle.Render("→"),
		tui.BrightStyle.Render(vaultName),
		tui.MutedStyle.Render(registryURL))

	results, conflicts, err := installer.InstallWithDeps(vaultName, cwd)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	for _, r := range results {
		printInstallResult(r.Result, cwd, r.DirectRequest)
	}

	if len(conflicts) > 0 {
		fmt.Printf("\n%s  version conflicts detected:\n", tui.ErrorStyle.Render("✗"))
		for _, c := range conflicts {
			fmt.Printf("   %s  %s\n", tui.ErrorStyle.Render("●"), c.Error())
		}
		return fmt.Errorf("%d version conflict(s) — review loompack.lock and resolve manually", len(conflicts))
	}

	fmt.Printf("\n%s  loompack.lock updated\n", tui.SuccessStyle.Render("✓"))
	return nil
}

func printInstallResult(r *installer.Result, cwd string, direct bool) {
	rel := func(p string) string {
		if s, err := filepath.Rel(cwd, p); err == nil {
			return s
		}
		return p
	}

	label := tui.SuccessStyle.Render("✓")
	if !direct {
		label = tui.MutedStyle.Render("↳") // transitive dependency
	}

	fmt.Println()
	fmt.Printf("%s  %s  %s\n",
		label,
		tui.PromptNameStyle.Render(r.Meta.Name),
		tui.MutedStyle.Render("v"+r.Meta.Version))

	if !direct {
		fmt.Printf("   %s\n", tui.MutedStyle.Render("(transitive dependency)"))
	}

	if r.Meta.Description != "" {
		fmt.Printf("   %s\n", tui.TextStyle.Render(r.Meta.Description))
	}
	if r.Meta.Author != "" {
		fmt.Printf("   %s %s\n", tui.MutedStyle.Render("by"), tui.TextStyle.Render(r.Meta.Author))
	}
	if len(r.Meta.Tags) > 0 {
		fmt.Printf("   %s %s\n",
			tui.MutedStyle.Render("tags:"),
			tui.MutedStyle.Render(strings.Join(r.Meta.Tags, ", ")))
	}

	fmt.Println()
	fmt.Printf("   %s  %s\n",
		tui.MutedStyle.Render("source   →"),
		tui.BrightStyle.Render(rel(r.SourceDir)))
	fmt.Printf("   %s  %s\n",
		tui.MutedStyle.Render("compiled →"),
		tui.BrightStyle.Render(rel(r.CompiledDir)))

	if len(r.Written) > 0 && direct {
		fmt.Printf("\n   %s\n", tui.SubHeaderStyle.Render("Source files"))
		for _, f := range r.Written {
			fmt.Printf("     %s %s\n", tui.BulletStyle.Render("●"), tui.TextStyle.Render(f))
		}
	}

	if len(r.Compiled) > 0 && direct {
		fmt.Printf("\n   %s\n", tui.SubHeaderStyle.Render("Compiled prompts"))
		for _, f := range r.Compiled {
			fmt.Printf("     %s %s\n", tui.SuccessStyle.Render("✓"), tui.TextStyle.Render(f))
		}
	}

	if direct {
		fmt.Println()
		fmt.Printf("   %s loom weave --from loompack/%s/source\n",
			tui.MutedStyle.Render("tip:"), r.Meta.Slug)
		fmt.Println()
	}
}

// resolveRegistryURL returns the registry base URL using the resolution order:
// shell env → loom/.loom.env file → default.
func resolveRegistryURL(dir string) string {
	if v := os.Getenv("LOOM_REGISTRY_URL"); v != "" {
		return v
	}
	if v := loomEnvValue(dir, "LOOM_REGISTRY_URL"); v != "" {
		return v
	}
	return "https://registry.promptloom.dev"
}

// loomEnvValue reads key from the first loom/.loom.env (or .loom.env) found
// by walking up from dir.
func loomEnvValue(dir string, key string) string {
	env := readLoomEnv(dir)
	return env[key]
}

func readLoomEnv(dir string) map[string]string {
	path := findLoomEnvFile(dir)
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		// Strip optional surrounding quotes.
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		result[k] = v
	}
	return result
}

func findLoomEnvFile(dir string) string {
	d := dir
	for {
		// Prefer new workspace layout: loom/.loom.env
		if p := filepath.Join(d, "loom", ".loom.env"); fileExists(p) {
			return p
		}
		// Fall back to legacy: .loom.env at project root
		if p := filepath.Join(d, ".loom.env"); fileExists(p) {
			return p
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
