package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/sayandeepgiri/promptloom/server/internal/models"
	"github.com/sayandeepgiri/promptloom/server/internal/validate"
)

// Store is the persistence layer the handlers depend on.
// GetVault and GetBundle return (nil, nil) when the pack does not exist.
type Store interface {
	ListVaults(ctx context.Context) ([]models.ListItem, error)
	GetVault(ctx context.Context, slug string) (*models.Vault, error)
	GetBundle(ctx context.Context, slug string) (*models.Bundle, error)
	UpsertVault(ctx context.Context, b *models.Bundle) error
	DeleteVault(ctx context.Context, slug string) error
}

// API bundles the HTTP handlers with the store they use.
type API struct {
	Store Store
}

// New returns an API backed by s.
func New(s Store) *API { return &API{Store: s} }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serverError logs the real cause and returns a generic 500 so database
// details are never leaked to clients.
func serverError(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("error: %s %s: %v", r.Method, r.URL.Path, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// slugParam extracts and validates the {slug} path value, writing a 400 on failure.
func slugParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	slug := r.PathValue("slug")
	if err := validate.Slug(slug); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return slug, true
}

// ListVaults handles GET /api/v1/vaults
func (a *API) ListVaults(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.ListVaults(r.Context())
	if err != nil {
		serverError(w, r, err)
		return
	}
	if items == nil {
		items = []models.ListItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"vaults": items})
}

// GetVault handles GET /api/v1/vaults/{slug}
func (a *API) GetVault(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	vault, err := a.Store.GetVault(r.Context(), slug)
	if err != nil {
		serverError(w, r, err)
		return
	}
	if vault == nil {
		writeError(w, http.StatusNotFound, "vault not found: "+slug)
		return
	}
	writeJSON(w, http.StatusOK, vault)
}

// GetBundle handles GET /api/v1/vaults/{slug}/bundle
func (a *API) GetBundle(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	bundle, err := a.Store.GetBundle(r.Context(), slug)
	if err != nil {
		serverError(w, r, err)
		return
	}
	if bundle == nil {
		writeError(w, http.StatusNotFound, "vault not found: "+slug)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// UploadVault handles POST /api/v1/vaults.
// Authentication, rate limiting and the body-size cap are applied by middleware.
func (a *API) UploadVault(w http.ResponseWriter, r *http.Request) {
	var bundle models.Bundle
	if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if problems := validate.Bundle(&bundle); len(problems) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "invalid pack",
			"details": problems,
		})
		return
	}

	if err := a.Store.UpsertVault(r.Context(), &bundle); err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "slug": bundle.Slug})
}

// DeleteVault handles DELETE /api/v1/vaults/{slug}.
// Authentication and rate limiting are applied by middleware.
func (a *API) DeleteVault(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugParam(w, r)
	if !ok {
		return
	}
	if err := a.Store.DeleteVault(r.Context(), slug); err != nil {
		serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "slug": slug})
}
