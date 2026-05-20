// Package packmetadata handles reading and writing .metadata.loom files,
// which are the v2 pack metadata format (JSON, replaces vault.toml).
package packmetadata

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Filename is the canonical name of the pack metadata file.
const Filename = ".metadata.loom"

// RelatedLibrary describes a library that pairs with this prompt pack.
type RelatedLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
}

// Metadata is the contents of a .metadata.loom file.
// ID is a UUID v4 that uniquely identifies this (slug, version) pair.
// Two versions of the same pack must have different IDs.
type Metadata struct {
	ID               string           `json:"id"`
	Slug             string           `json:"slug"`
	Version          string           `json:"version"`
	Name             string           `json:"name"`
	Author           string           `json:"author,omitempty"`
	Description      string           `json:"description,omitempty"`
	Tags             []string         `json:"tags,omitempty"`
	RelatedLibraries []RelatedLibrary `json:"relatedLibraries,omitempty"`
}

// Read parses the .metadata.loom file in packDir.
func Read(packDir string) (*Metadata, error) {
	data, err := os.ReadFile(filepath.Join(packDir, Filename))
	if err != nil {
		return nil, err
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", Filename, err)
	}
	return &m, nil
}

// Write serialises m to .metadata.loom in packDir, generating an ID if absent.
func Write(packDir string, m *Metadata) error {
	m.EnsureID()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(packDir, Filename), data, 0644)
}

// Validate returns an error if required fields are missing.
func (m *Metadata) Validate() error {
	if m.Slug == "" {
		return fmt.Errorf("%s: slug is required", Filename)
	}
	if m.Name == "" {
		return fmt.Errorf("%s: name is required", Filename)
	}
	if m.Version == "" {
		return fmt.Errorf("%s: version is required", Filename)
	}
	return nil
}

// EnsureID sets a new UUID v4 ID if m.ID is empty.
func (m *Metadata) EnsureID() {
	if m.ID == "" {
		m.ID = NewID()
	}
}

// NewID generates a UUID v4 string.
func NewID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]))
}
