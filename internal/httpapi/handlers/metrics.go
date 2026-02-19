package handlers

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yourorg/fap/internal/obs"
)

func Metrics(w http.ResponseWriter, r *http.Request) {
	obs.Register()
	promhttp.Handler().ServeHTTP(w, r)
}
