package cli

import (
	"bufio"
	"fmt"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
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
  4. [registry] url in loom.toml

PromptLoom has no built-in default registry. Run your own (see server/ in the
repository) or use one your team provides, then set its URL with one of the above.

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

	registryURL, registrySource := resolveRegistryURL(cwd)
	if installRegistry != "" {
		registryURL = installRegistry
		registrySource = "--registry flag"
	}
	if registryURL == "" {
		return errNoRegistry()
	}
	if err := checkRegistryURL(registryURL); err != nil {
		return err
	}
	if isPlainHTTPRemote(registryURL) {
		fmt.Fprintf(os.Stderr, "%s  registry %s uses plain HTTP — packs are downloaded unencrypted and unauthenticated\n",
			tui.MutedStyle.Render("!"), registryURL)
	}
	// Propagate so installer.Install picks it up via os.Getenv.
	os.Setenv("LOOM_REGISTRY_URL", registryURL)

	fmt.Printf("%s  fetching %s from %s  %s\n",
		tui.MutedStyle.Render("→"),
		tui.BrightStyle.Render(vaultName),
		tui.MutedStyle.Render(registryURL),
		tui.MutedStyle.Render("("+registrySource+")"))

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

// resolveRegistryURL returns the registry base URL and its source label, or
// ("", "") when none is configured — there is deliberately no built-in default.
// Order: shell env → loom/.loom.env → [registry] url in loom.toml.
// (The --registry flag is applied by the callers and takes precedence.)
func resolveRegistryURL(dir string) (url, source string) {
	if v := strings.TrimSpace(os.Getenv("LOOM_REGISTRY_URL")); v != "" {
		return v, "$LOOM_REGISTRY_URL"
	}
	if path := findLoomEnvFile(dir); path != "" {
		env := readLoomEnvFromPath(path)
		if v := strings.TrimSpace(env["LOOM_REGISTRY_URL"]); v != "" {
			return v, path
		}
	}
	if root, ok := config.FindProjectRoot(dir); ok {
		if cfg, err := config.Load(root); err == nil {
			if v := strings.TrimSpace(cfg.Registry.URL); v != "" {
				return v, filepath.Join(root, "loom.toml")
			}
		}
	}
	return "", ""
}

// errNoRegistry explains how to configure a registry.
func errNoRegistry() error {
	return fmt.Errorf(`no registry configured.

PromptLoom does not ship with a default registry. Point it at one:

  loom install <pack> --registry https://registry.example.com     (one-off)
  export LOOM_REGISTRY_URL=https://registry.example.com           (shell)
  echo 'LOOM_REGISTRY_URL=https://registry.example.com' >> loom/.loom.env   (project)

  [registry]                                                       (loom.toml)
  url = "https://registry.example.com"

Don't have one? Run your own — see "Registry server" in the README (server/).
For local testing: cd server && go run .  then use http://localhost:8080`)
}

// checkRegistryURL rejects registry URLs that are not absolute http(s) URLs.
func checkRegistryURL(raw string) error {
	u, err := neturl.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("invalid registry URL %q: use an absolute http:// or https:// URL", raw)
	}
	return nil
}

// isPlainHTTPRemote reports whether raw is http:// to a non-loopback host.
func isPlainHTTPRemote(raw string) bool {
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}

// loomEnvValue reads key from the first loom/.loom.env (or .loom.env) found
// by walking up from dir.
func readLoomEnvFromPath(path string) map[string]string {
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
