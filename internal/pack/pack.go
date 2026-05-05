// Package pack implements PromptLoom pack operations: init, build, install,
// list, and remove.
package pack

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
func Build(cwd string) (string, error) {
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

	archiveName := fmt.Sprintf("%s-%s%s", m.Pack.Name, m.Pack.Version, packExt)
	archivePath := filepath.Join(cwd, archiveName)

	f, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("creating archive: %w", err)
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// Include pack.toml at the archive root.
	if err := addFile(tw, filepath.Join(cwd, manifestFile), manifestFile); err != nil {
		return "", err
	}

	// Include prompts/ and blocks/ if they exist.
	for _, subdir := range []string{"prompts", "blocks"} {
		dir := filepath.Join(cwd, subdir)
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		if err := addDir(tw, dir, subdir); err != nil {
			return "", err
		}
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

	// First pass: extract pack.toml to get the pack name.
	var manifest *Manifest
	var entries []struct {
		header  *tar.Header
		content []byte
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive: %w", err)
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("reading %s: %w", hdr.Name, err)
		}
		if hdr.Name == manifestFile {
			var m Manifest
			if _, err := toml.Decode(string(data), &m); err != nil {
				return fmt.Errorf("parsing pack.toml: %w", err)
			}
			manifest = &m
		}
		entries = append(entries, struct {
			header  *tar.Header
			content []byte
		}{hdr, data})
	}

	if manifest == nil {
		return fmt.Errorf("archive does not contain a pack.toml")
	}
	if manifest.Pack.Name == "" {
		return fmt.Errorf("pack.toml: name is required")
	}
	packName := manifest.Pack.Name

	// Create destination directories.
	for _, subdir := range []string{
		filepath.Join("prompts", packName),
		filepath.Join("blocks", packName),
		packsDir,
	} {
		if err := os.MkdirAll(filepath.Join(targetCWD, subdir), 0755); err != nil {
			return err
		}
	}

	// Second pass: write files.
	for _, e := range entries {
		name := e.header.Name
		var dest string
		switch {
		case name == manifestFile:
			dest = filepath.Join(targetCWD, packsDir, packName+".toml")
		case strings.HasPrefix(name, "prompts/"):
			rel := strings.TrimPrefix(name, "prompts/")
			dest = filepath.Join(targetCWD, "prompts", packName, rel)
		case strings.HasPrefix(name, "blocks/"):
			rel := strings.TrimPrefix(name, "blocks/")
			dest = filepath.Join(targetCWD, "blocks", packName, rel)
		default:
			continue
		}
		if e.header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, e.content, 0644); err != nil {
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
		archivePath := filepath.Join(archivePrefix, rel)

		if info.IsDir() {
			return tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir,
				Name:     archivePath + "/",
				Mode:     0755,
			})
		}
		return addFile(tw, path, archivePath)
	})
}
