package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/packmetadata"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

// bundleFile matches the server's models.BundleFile wire format.
type bundleFile struct {
	Path     string `json:"path"`
	FileType string `json:"file_type"`
	Content  string `json:"content"`
}

// relatedLibrary matches models.RelatedLibrary.
type relatedLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
}

// uploadBundle matches the server's models.Bundle wire format.
type uploadBundle struct {
	PackID           string           `json:"pack_id,omitempty"`
	Name             string           `json:"name"`
	Slug             string           `json:"slug"`
	Version          string           `json:"version"`
	Description      string           `json:"description"`
	Author           string           `json:"author"`
	Tags             []string         `json:"tags"`
	RelatedLibraries []relatedLibrary `json:"relatedLibraries,omitempty"`
	Files            []bundleFile     `json:"files"`
}

var publishRegistry string
var publishSecret string
var publishDryRun bool

var publishCmd = &cobra.Command{
	Use:   "publish <pack-dir>",
	Short: "Upload a prompt-pack to the registry",
	Long: `Read a pack directory (containing .metadata.loom + prompts/, blocks/, overlays/)
and upload it to the PromptLoom registry.

Pack structure:
  <pack>/
    prompts/         ← .prompt.loom files
    blocks/          ← .block.loom files
    overlays/        ← .overlay.loom files
    .metadata.loom   ← pack metadata (JSON)
    .dependency.loom ← pack dependencies
    .export.loom     ← public API declarations

Registry URL: --registry flag → $LOOM_REGISTRY_URL → loom/.loom.env → default.
Upload secret: --secret flag → $UPLOAD_SECRET.

Examples:
  loom publish examples/go-backend-pack
  loom publish ./my-pack --registry http://localhost:8080
  loom publish ./my-pack --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func init() {
	publishCmd.Flags().StringVar(&publishRegistry, "registry", "",
		"registry base URL (overrides $LOOM_REGISTRY_URL and .loom.env)")
	publishCmd.Flags().StringVar(&publishSecret, "secret", "",
		"upload secret (overrides $UPLOAD_SECRET)")
	publishCmd.Flags().BoolVar(&publishDryRun, "dry-run", false,
		"show what would be uploaded without sending")
}

func runPublish(cmd *cobra.Command, args []string) error {
	packDir := args[0]
	if !filepath.IsAbs(packDir) {
		cwd, _ := os.Getwd()
		packDir = filepath.Join(cwd, packDir)
	}

	// Read .metadata.loom.
	meta, err := packmetadata.Read(packDir)
	if err != nil {
		return fmt.Errorf(".metadata.loom not found in %s — pack must have a .metadata.loom file\n"+
			"  (old vault.toml format is no longer supported; see docs/PACKMAKER_DESIGN.md)", packDir)
	}
	if err := meta.Validate(); err != nil {
		return err
	}
	// Generate ID if missing — warn the author it will change on next publish if not saved.
	if meta.ID == "" {
		meta.ID = packmetadata.NewID()
		fmt.Printf("%s  .metadata.loom has no id — generated %s for this upload\n"+
			"       Save it back to .metadata.loom to keep stable across republishes.\n",
			tui.WarningStyle.Render("⚠"), tui.MutedStyle.Render(meta.ID))
	}

	// Collect all files: prompts/, blocks/, overlays/, and meta dotfiles.
	files, err := collectPackFiles(packDir)
	if err != nil {
		return fmt.Errorf("scan pack dir: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no files found in %s", packDir)
	}

	// Convert relatedLibraries.
	var relLibs []relatedLibrary
	for _, rl := range meta.RelatedLibraries {
		relLibs = append(relLibs, relatedLibrary{Name: rl.Name, Version: rl.Version, ID: rl.ID})
	}

	bundle := uploadBundle{
		PackID:           meta.ID,
		Name:             meta.Name,
		Slug:             meta.Slug,
		Version:          meta.Version,
		Description:      meta.Description,
		Author:           meta.Author,
		Tags:             meta.Tags,
		RelatedLibraries: relLibs,
		Files:            files,
	}

	// Print preview.
	fmt.Printf("%s  %s  %s\n",
		tui.PromptNameStyle.Render(bundle.Name),
		tui.MutedStyle.Render("v"+bundle.Version),
		tui.MutedStyle.Render("slug: "+bundle.Slug))
	if bundle.Description != "" {
		fmt.Printf("   %s\n", tui.TextStyle.Render(bundle.Description))
	}
	fmt.Printf("   %s %s\n", tui.MutedStyle.Render("id:"), tui.MutedStyle.Render(bundle.PackID))
	fmt.Printf("\n   %s\n", tui.SubHeaderStyle.Render("Files to upload"))
	for _, f := range files {
		badge := tui.BulletStyle.Render("●")
		switch f.FileType {
		case "block":
			badge = tui.BlockNameStyle.Render("□")
		case "meta":
			badge = tui.MutedStyle.Render("·")
		}
		fmt.Printf("     %s %s %s\n", badge,
			tui.TextStyle.Render(f.Path),
			tui.MutedStyle.Render("["+f.FileType+"]"))
	}
	fmt.Println()

	if publishDryRun {
		fmt.Printf("%s  dry-run — nothing uploaded.\n", tui.MutedStyle.Render("→"))
		return nil
	}

	// Resolve registry URL (flag > env > .loom.env > default).
	cwd, _ := os.Getwd()
	registryURL := publishRegistry
	if registryURL == "" {
		registryURL = resolveRegistryURL(cwd)
	}
	registryURL = strings.TrimRight(registryURL, "/")

	secret := publishSecret
	if secret == "" {
		secret = os.Getenv("UPLOAD_SECRET")
	}

	fmt.Printf("%s  uploading to %s…\n",
		tui.MutedStyle.Render("→"), tui.MutedStyle.Render(registryURL))

	if err := uploadToRegistry(registryURL, secret, bundle); err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}

	fmt.Printf("%s  published %s successfully\n",
		tui.SuccessStyle.Render("✓"),
		tui.BrightStyle.Render(bundle.Slug))
	fmt.Printf("   install with: %s\n",
		tui.PromptNameStyle.Render("loom install "+bundle.Slug))
	return nil
}

// collectPackFiles walks a v2 pack directory, collecting all relevant files.
// Files in prompts/, blocks/, overlays/ are included; meta dotfiles are included.
// file_type is inferred from the directory prefix first, then the extension.
func collectPackFiles(packDir string) ([]bundleFile, error) {
	var files []bundleFile
	err := filepath.WalkDir(packDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(packDir, path)
		rel = filepath.ToSlash(rel) // normalise to forward slashes in bundle

		ft := inferFileTypeV2(rel, d.Name())
		if ft == "" {
			return nil // skip unrecognised files
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, bundleFile{Path: rel, FileType: ft, Content: string(content)})
		return nil
	})
	return files, err
}

// inferFileTypeV2 determines file_type from the relative path and filename.
// Returns "" to skip the file.
func inferFileTypeV2(rel, name string) string {
	// Meta dotfiles at pack root.
	switch name {
	case ".metadata.loom", ".dependency.loom", ".export.loom":
		return "meta"
	case "loom.toml":
		return "meta"
	}
	// Directory-based inference.
	switch {
	case strings.HasPrefix(rel, "prompts/"):
		return "prompt"
	case strings.HasPrefix(rel, "blocks/"):
		return "block"
	case strings.HasPrefix(rel, "overlays/"):
		return "overlay"
	}
	// Extension fallback for flat-layout files (legacy).
	if strings.HasSuffix(name, ".loom") {
		switch {
		case strings.HasSuffix(name, ".block.loom"):
			return "block"
		case strings.HasSuffix(name, ".overlay.loom"):
			return "overlay"
		case strings.HasSuffix(name, ".prompt.loom"):
			return "prompt"
		}
	}
	return ""
}

func uploadToRegistry(registryURL, secret string, bundle uploadBundle) error {
	body, err := json.Marshal(bundle)
	if err != nil {
		return err
	}

	url := registryURL + "/api/v1/vaults"
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Upload-Secret", secret)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized — check --secret or $UPLOAD_SECRET")
	}
	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return fmt.Errorf("server error (%d): %s", resp.StatusCode, errBody.Error)
		}
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}
