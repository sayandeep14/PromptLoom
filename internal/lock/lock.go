// Package lock implements the loom.lock fingerprint lockfile.
package lock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// PromptEntry is one prompt's lockfile record.
type PromptEntry struct {
	Name   string   `toml:"name"`
	Hash   string   `toml:"hash"`
	Blocks []string `toml:"blocks,omitempty"`
}

// BlockEntry is one block's lockfile record.
type BlockEntry struct {
	Name string `toml:"name"`
	Hash string `toml:"hash"`
}

// Lockfile is the in-memory representation of loom.lock.
type Lockfile struct {
	Prompts []PromptEntry `toml:"prompt"`
	Blocks  []BlockEntry  `toml:"block"`
}

// Path returns the absolute path of loom.lock for the given project root.
func Path(cwd string) string {
	return filepath.Join(cwd, "loom.lock")
}

// Generate builds a Lockfile by resolving all prompts and hashing all block source files.
func Generate(reg *registry.Registry, cwd string) (*Lockfile, error) {
	lf := &Lockfile{}

	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
	for _, p := range prompts {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", p.Name, err)
		}
		entry := PromptEntry{
			Name:   p.Name,
			Hash:   rp.Fingerprint,
			Blocks: rp.UsedBlocks,
		}
		if len(entry.Blocks) == 0 {
			entry.Blocks = nil
		}
		lf.Prompts = append(lf.Prompts, entry)
	}

	blocks := reg.Blocks()
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Name < blocks[j].Name })
	for _, b := range blocks {
		if b.Pos.File == "" {
			continue
		}
		src, err := os.ReadFile(b.Pos.File)
		if err != nil {
			return nil, fmt.Errorf("reading block %s: %w", b.Name, err)
		}
		sum := sha256.Sum256(src)
		lf.Blocks = append(lf.Blocks, BlockEntry{
			Name: b.Name,
			Hash: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}

	return lf, nil
}

// Write serialises the lockfile to loom.lock in cwd.
func Write(lf *Lockfile, cwd string) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(lf); err != nil {
		return err
	}
	return os.WriteFile(Path(cwd), buf.Bytes(), 0644)
}

// Read parses loom.lock from cwd.
func Read(cwd string) (*Lockfile, error) {
	var lf Lockfile
	if _, err := toml.DecodeFile(Path(cwd), &lf); err != nil {
		return nil, fmt.Errorf("loom.lock not found — run: loom lock")
	}
	return &lf, nil
}

// Mismatch describes one entry that differs between the lockfile and current state.
type Mismatch struct {
	Name    string
	Kind    string // "prompt" or "block"
	Locked  string
	Current string
}

// Check generates the current state and reports mismatches against the lockfile.
func Check(reg *registry.Registry, cwd string) ([]Mismatch, error) {
	locked, err := Read(cwd)
	if err != nil {
		return nil, err
	}
	current, err := Generate(reg, cwd)
	if err != nil {
		return nil, err
	}

	var mismatches []Mismatch

	lockedPrompts := make(map[string]string, len(locked.Prompts))
	for _, p := range locked.Prompts {
		lockedPrompts[p.Name] = p.Hash
	}
	currentPrompts := make(map[string]string, len(current.Prompts))
	for _, p := range current.Prompts {
		currentPrompts[p.Name] = p.Hash
	}
	for _, p := range current.Prompts {
		if lh, ok := lockedPrompts[p.Name]; !ok {
			mismatches = append(mismatches, Mismatch{Name: p.Name, Kind: "prompt", Locked: "(new)", Current: p.Hash})
		} else if lh != p.Hash {
			mismatches = append(mismatches, Mismatch{Name: p.Name, Kind: "prompt", Locked: lh, Current: p.Hash})
		}
	}
	for _, p := range locked.Prompts {
		if _, ok := currentPrompts[p.Name]; !ok {
			mismatches = append(mismatches, Mismatch{Name: p.Name, Kind: "prompt", Locked: p.Hash, Current: "(removed)"})
		}
	}

	lockedBlocks := make(map[string]string, len(locked.Blocks))
	for _, b := range locked.Blocks {
		lockedBlocks[b.Name] = b.Hash
	}
	currentBlocks := make(map[string]string, len(current.Blocks))
	for _, b := range current.Blocks {
		currentBlocks[b.Name] = b.Hash
	}
	for _, b := range current.Blocks {
		if lh, ok := lockedBlocks[b.Name]; !ok {
			mismatches = append(mismatches, Mismatch{Name: b.Name, Kind: "block", Locked: "(new)", Current: b.Hash})
		} else if lh != b.Hash {
			mismatches = append(mismatches, Mismatch{Name: b.Name, Kind: "block", Locked: lh, Current: b.Hash})
		}
	}
	for _, b := range locked.Blocks {
		if _, ok := currentBlocks[b.Name]; !ok {
			mismatches = append(mismatches, Mismatch{Name: b.Name, Kind: "block", Locked: b.Hash, Current: "(removed)"})
		}
	}

	return mismatches, nil
}
