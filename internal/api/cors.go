package api

import (
	"net/http"
	"strings"
)

const (
	corsAllowMethods  = "GET, OPTIONS"
	corsAllowHeaders  = "Authorization, Range, If-None-Match, If-Modified-Since, Content-Type"
	corsExposeHeaders = "Content-Length"
)

func NewHLSCORSMiddleware(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/hls/") {
				next.ServeHTTP(w, r)
				return
			}

			origin := strings.TrimSpace(r.Header.Get("Origin"))
			isAllowed := originAllowed(origin, allowed)

			if r.Method == http.MethodOptions {
				if !isAllowed {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				setCORSHeaders(w.Header(), origin, allowCredentials)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			if isAllowed {
				setCORSHeaders(w.Header(), origin, allowCredentials)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func originAllowed(origin string, allowed map[string]struct{}) bool {
	if origin == "" {
		return false
	}
	_, ok := allowed[origin]
	return ok
}

func setCORSHeaders(h http.Header, origin string, allowCredentials bool) {
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Methods", corsAllowMethods)
	h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
	h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
	h.Set("Vary", "Origin")
	if allowCredentials {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
}
