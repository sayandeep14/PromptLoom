// Package pack implements PromptLoom pack operations: init, build, install,
// list, and remove.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// Manifest is the content of a pack.toml file.
type Manifest struct {
	Pack Info `toml:"pack"`
}

// Info holds the pack metadata.
type Info struct {
	Name        string `toml:"name"`
	Version     string `toml:"version"`
	Description string `toml:"description"`
	Author      string `toml:"author"`
	License     string `toml:"license"`
}

// InstalledPack describes a pack installed in a project.
type InstalledPack struct {
	Name    string
	Version string
	Path    string // path to the pack's manifest inside the project
}

const manifestFile = "pack.toml"
const packExt = ".lpack"
const packsDir = "packs"

// Limits applied when unpacking an archive (a small .lpack can expand enormously).
const (
	maxEntries   = 5000
	maxFileBytes = 4 << 20  // 4 MiB per file
	maxTotalSize = 64 << 20 // 64 MiB overall
)

var (
	nameRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	versionRe = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$`)
)

// ValidateName checks a pack name. Names become directory names (prompts/<name>/) and part
// of the archive file name, so they must not contain path separators or start with a dot:
// `loom pack remove ..` would otherwise resolve to the project root.
func ValidateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid pack name %q: use letters, digits, '.', '_' or '-' (up to 64 characters, not starting with a dot)", name)
	}
	return nil
}

// safeRel validates an archive-relative path that must stay inside its directory.
func safeRel(rel string) error {
	switch {
	case rel == "":
		return fmt.Errorf("empty path")
	case strings.ContainsAny(rel, "\\\x00"):
		return fmt.Errorf("illegal character in path")
	case strings.HasPrefix(rel, "/"):
		return fmt.Errorf("absolute path")
	}
	clean := path.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." && rel != "." {
		return fmt.Errorf("path escapes its directory")
	}
	return nil
}

// Init creates a pack.toml scaffold in cwd.
func Init(cwd string) error {
	dest := filepath.Join(cwd, manifestFile)
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("pack.toml already exists")
	}
	content := `[pack]
name        = "my-pack"
version     = "1.0.0"
description = "A collection of reusable prompts and blocks"
author      = ""
license     = "MIT"
`
	return os.WriteFile(dest, []byte(content), 0644)
}

// Build creates a .lpack archive in cwd from prompts/, blocks/, and pack.toml.
// Returns the path to the created archive.
func Build(cwd string) (archive string, err error) {
	m, err := LoadManifest(cwd)
	if err != nil {
		return "", err
	}
	if m.Pack.Name == "" {
		return "", fmt.Errorf("pack.toml: name is required")
	}
	if m.Pack.Version == "" {
		return "", fmt.Errorf("pack.toml: version is required")
	}
	if err := ValidateName(m.Pack.Name); err != nil {
		return "", fmt.Errorf("pack.toml: %w", err)
	}
	if !versionRe.MatchString(m.Pack.Version) {
		return "", fmt.Errorf("pack.toml: invalid version %q", m.Pack.Version)
	}

	archiveName := fmt.Sprintf("%s-%s%s", m.Pack.Name, m.Pack.Version, packExt)
	archivePath := filepath.Join(cwd, archiveName)

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("creating archive: %w", err)
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(archivePath) // never leave a half-written archive behind
		}
	}()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// Include pack.toml at the archive root.
	if err = addFile(tw, filepath.Join(cwd, manifestFile), manifestFile); err != nil {
		return "", err
	}

	// Include prompts/ and blocks/ if they exist.
	for _, subdir := range []string{"prompts", "blocks"} {
		dir := filepath.Join(cwd, subdir)
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		if err = addDir(tw, dir, subdir); err != nil {
			return "", err
		}
	}
	if err = tw.Close(); err != nil {
		return "", err
	}
	if err = gz.Close(); err != nil {
		return "", err
	}

	return archivePath, nil
}

// Install unpacks an .lpack archive into the target project directory.
// Prompts and blocks are placed under prompts/<pack-name>/ and blocks/<pack-name>/.
// The pack manifest is copied to packs/<pack-name>.toml.
func Install(archivePath, targetCWD string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	// First pass: read and VALIDATE every entry before anything is written, so a bad
	// archive changes nothing on disk.
	var manifest *Manifest
	type entry struct {
		name    string
		isDir   bool
		content []byte
	}
	var entries []entry
	var total int64

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		if len(entries) >= maxEntries {
			return fmt.Errorf("archive has too many entries (limit %d)", maxEntries)
		}
		name := strings.TrimSuffix(hdr.Name, "/")
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeDir:
		case tar.TypeXGlobalHeader:
			continue
		default:
			return fmt.Errorf("archive entry %q is a link or special file, which packs must not contain", hdr.Name)
		}
		if name != manifestFile {
			if err := safeRel(name); err != nil {
				return fmt.Errorf("unsafe archive entry %q: %w", hdr.Name, err)
			}
		}
		if hdr.Typeflag == tar.TypeDir {
			entries = append(entries, entry{name: name, isDir: true})
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxFileBytes+1))
		if err != nil {
			return fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		if len(data) > maxFileBytes {
			return fmt.Errorf("archive entry %q is larger than %d bytes", hdr.Name, maxFileBytes)
		}
		if total += int64(len(data)); total > maxTotalSize {
			return fmt.Errorf("archive expands to more than %d bytes", maxTotalSize)
		}
		if name == manifestFile {
			var m Manifest
			if _, err := toml.Decode(string(data), &m); err != nil {
				return fmt.Errorf("parsing pack.toml: %w", err)
			}
			manifest = &m
		}
		entries = append(entries, entry{name: name, content: data})
	}

	if manifest == nil {
		return fmt.Errorf("archive does not contain a pack.toml")
	}
	if manifest.Pack.Name == "" {
		return fmt.Errorf("pack.toml: name is required")
	}
	if err := ValidateName(manifest.Pack.Name); err != nil {
		return fmt.Errorf("pack.toml: %w", err)
	}
	packName := manifest.Pack.Name

	promptsRoot := filepath.Join(targetCWD, "prompts", packName)
	blocksRoot := filepath.Join(targetCWD, "blocks", packName)

	// Resolve every destination up front and confirm it stays inside the pack's own directory.
	type write struct {
		dest    string
		isDir   bool
		content []byte
	}
	var writes []write
	within := func(root, dest string) bool {
		rel, err := filepath.Rel(root, dest)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	for _, e := range entries {
		var dest, root string
		switch {
		case e.name == manifestFile:
			dest = filepath.Join(targetCWD, packsDir, packName+".toml")
			writes = append(writes, write{dest: dest, content: e.content})
			continue
		case strings.HasPrefix(e.name, "prompts/") || e.name == "prompts":
			root, dest = promptsRoot, filepath.Join(promptsRoot, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(e.name, "prompts"), "/")))
		case strings.HasPrefix(e.name, "blocks/") || e.name == "blocks":
			root, dest = blocksRoot, filepath.Join(blocksRoot, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(e.name, "blocks"), "/")))
		default:
			continue
		}
		if !within(root, dest) {
			return fmt.Errorf("unsafe archive entry %q: resolves outside %s", e.name, root)
		}
		writes = append(writes, write{dest: dest, isDir: e.isDir, content: e.content})
	}

	// Second pass: write.
	for _, subdir := range []string{filepath.Join("prompts", packName), filepath.Join("blocks", packName), packsDir} {
		if err := os.MkdirAll(filepath.Join(targetCWD, subdir), 0755); err != nil {
			return err
		}
	}
	for _, w := range writes {
		if w.isDir {
			if err := os.MkdirAll(w.dest, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(w.dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(w.dest, w.content, 0644); err != nil {
			return err
		}
	}

	return nil
}

// List returns all packs installed in the target project.
func List(cwd string) ([]InstalledPack, error) {
	dir := filepath.Join(cwd, packsDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading packs dir: %w", err)
	}

	var out []InstalledPack
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".toml" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var m Manifest
		if _, err := toml.Decode(string(data), &m); err != nil {
			continue
		}
		out = append(out, InstalledPack{
			Name:    m.Pack.Name,
			Version: m.Pack.Version,
			Path:    path,
		})
	}
	return out, nil
}

// Remove deletes a pack and all its prompts and blocks from the project.
func Remove(name, cwd string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	removed := false
	for _, subdir := range []string{
		filepath.Join("prompts", name),
		filepath.Join("blocks", name),
	} {
		path := filepath.Join(cwd, subdir)
		if _, err := os.Stat(path); err == nil {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
			removed = true
		}
	}

	manifest := filepath.Join(cwd, packsDir, name+".toml")
	if _, err := os.Stat(manifest); err == nil {
		if err := os.Remove(manifest); err != nil {
			return fmt.Errorf("removing manifest: %w", err)
		}
		removed = true
	}

	if !removed {
		return fmt.Errorf("pack %q is not installed", name)
	}
	return nil
}

// LoadManifest reads and parses the pack.toml in dir.
func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, fmt.Errorf("could not read pack.toml: %w", err)
	}
	var m Manifest
	if _, err := toml.Decode(string(data), &m); err != nil {
		return nil, fmt.Errorf("could not parse pack.toml: %w", err)
	}
	return &m, nil
}

// addFile writes a single file into the tar archive at archivePath.
func addFile(tw *tar.Writer, srcPath, archivePath string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", srcPath, err)
	}
	hdr := &tar.Header{
		Name: archivePath,
		Mode: 0644,
		Size: int64(len(data)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = tw.Write(data)
	return err
}

// addDir recursively adds a directory to the tar archive.
func addDir(tw *tar.Writer, srcDir, archivePrefix string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		archivePath := filepath.ToSlash(filepath.Join(archivePrefix, rel))

		// A symlink could point at any file on this machine (a key, a .env) and would
		// otherwise be followed and published inside the archive.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     archivePath + "/",
				Mode:     0755,
			})
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return addFile(tw, path, archivePath)
	})
}
