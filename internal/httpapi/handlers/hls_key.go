package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/yourorg/fap/internal/hls"
)

func (a *API) HLSKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	assetID, ok := parseHLSKeyAssetID(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if a.hlsKeys == nil {
		writeError(w, http.StatusInternalServerError, "key storage unavailable")
		return
	}

	key, err := a.hlsKeys.GetKey(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, hls.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load hls key")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(key)
}

func parseHLSKeyAssetID(path string) (string, bool) {
	if !strings.HasPrefix(path, "/hls/") || !strings.HasSuffix(path, "/key") {
		return "", false
	}

	trimmed := strings.TrimPrefix(path, "/hls/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", false
	}
	if parts[0] == "" || parts[1] != "key" {
		return "", false
	}
	return parts[0], true
}
