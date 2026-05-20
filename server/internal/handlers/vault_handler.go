package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/sayandeepgiri/promptloom/server/internal/models"
	"github.com/sayandeepgiri/promptloom/server/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ListVaults handles GET /api/v1/vaults
func ListVaults(w http.ResponseWriter, r *http.Request) {
	items, err := store.ListVaults(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []models.ListItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"vaults": items})
}

// GetVault handles GET /api/v1/vaults/{slug}
func GetVault(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	vault, err := store.GetVault(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vault == nil {
		writeError(w, http.StatusNotFound, "vault not found: "+slug)
		return
	}
	writeJSON(w, http.StatusOK, vault)
}

// GetBundle handles GET /api/v1/vaults/{slug}/bundle
func GetBundle(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	bundle, err := store.GetBundle(r.Context(), slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if bundle == nil {
		writeError(w, http.StatusNotFound, "vault not found: "+slug)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

// UploadVault handles POST /api/v1/vaults — requires UPLOAD_SECRET header.
func UploadVault(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("UPLOAD_SECRET")
	if secret != "" {
		auth := r.Header.Get("X-Upload-Secret")
		if !strings.EqualFold(auth, secret) {
			writeError(w, http.StatusUnauthorized, "invalid upload secret")
			return
		}
	}

	var bundle models.Bundle
	if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if bundle.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if len(bundle.Files) == 0 {
		writeError(w, http.StatusBadRequest, "files list is empty")
		return
	}

	if err := store.UpsertVault(r.Context(), &bundle); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "slug": bundle.Slug})
}

// DeleteVault handles DELETE /api/v1/vaults/{slug} — requires UPLOAD_SECRET header.
func DeleteVault(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("UPLOAD_SECRET")
	if secret != "" {
		auth := r.Header.Get("X-Upload-Secret")
		if !strings.EqualFold(auth, secret) {
			writeError(w, http.StatusUnauthorized, "invalid upload secret")
			return
		}
	}

	slug := r.PathValue("slug")
	if err := store.DeleteVault(r.Context(), slug); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "slug": slug})
}
