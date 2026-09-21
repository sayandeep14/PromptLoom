package models

import (
	"errors"
	"time"
)

// Identity is who is making a write request. Admin may manage every pack; everyone else only
// the packs they published.
type Identity struct {
	Name  string
	Admin bool
}

// ErrNotOwner is returned when a publisher tries to change a pack that belongs to someone else.
var ErrNotOwner = errors.New("pack is owned by another publisher")

// RelatedLibrary describes a library that pairs with a prompt pack.
type RelatedLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	ID      string `json:"id"`
}

// Vault is a named collection of .loom prompt files published to the registry.
type Vault struct {
	ID               string           `json:"id"`
	PackID           string           `json:"pack_id,omitempty"`
	Name             string           `json:"name"`
	Slug             string           `json:"slug"`
	Description      string           `json:"description"`
	Author           string           `json:"author"`
	Owner            string           `json:"owner,omitempty"` // publisher identity, not the free-text author
	Version          string           `json:"version"`
	Tags             []string         `json:"tags"`
	RelatedLibraries []RelatedLibrary `json:"relatedLibraries,omitempty"`
	FileCount        int              `json:"file_count"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// VaultFile is a single source file belonging to a vault.
// file_type is "prompt" | "block" | "overlay" | "meta"
type VaultFile struct {
	ID       string `json:"id"`
	VaultID  string `json:"vault_id"`
	Path     string `json:"path"`
	FileType string `json:"file_type"`
	Content  string `json:"content"`
}

// Bundle is the wire format returned by GET /api/v1/vaults/{slug}/bundle.
// The client writes the source files and compiles them locally.
type Bundle struct {
	PackID           string           `json:"pack_id,omitempty"`
	Name             string           `json:"name"`
	Slug             string           `json:"slug"`
	Version          string           `json:"version"`
	Description      string           `json:"description"`
	Author           string           `json:"author"`
	Tags             []string         `json:"tags"`
	RelatedLibraries []RelatedLibrary `json:"relatedLibraries,omitempty"`
	Files            []BundleFile     `json:"files"`
}

// BundleFile is one file entry inside a Bundle.
type BundleFile struct {
	Path     string `json:"path"`
	FileType string `json:"file_type"`
	Content  string `json:"content"`
}

// ListItem is the compact representation used in the vault list response.
type ListItem struct {
	PackID      string    `json:"pack_id,omitempty"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	Owner       string    `json:"owner,omitempty"`
	Version     string    `json:"version"`
	Tags        []string  `json:"tags"`
	FileCount   int       `json:"file_count"`
	UpdatedAt   time.Time `json:"updated_at"`
}
