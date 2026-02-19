package handlers

import (
	"encoding/json"
	"net/http"
)

type HealthzResponse struct {
	OK bool `json:"ok"`
}

func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthzResponse{OK: true})
}
