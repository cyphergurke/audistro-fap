package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
)

type OpenAPIValidateConfig struct {
	Disabled        bool
	LoadSpec        func() (*openapi3.T, error)
	IncludePrefixes []string
	SkipPaths       map[string]struct{}
}

type openAPIValidationErrorResponse struct {
	Error   string         `json:"error"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func OpenAPIValidate(cfg OpenAPIValidateConfig) func(http.Handler) http.Handler {
	if cfg.Disabled {
		return func(next http.Handler) http.Handler { return next }
	}
	if cfg.LoadSpec == nil {
		panic("openapi validation requires LoadSpec")
	}
	if len(cfg.IncludePrefixes) == 0 {
		cfg.IncludePrefixes = []string{"/v1/", "/hls/", "/internal/"}
	}
	if cfg.SkipPaths == nil {
		cfg.SkipPaths = map[string]struct{}{}
	}
	cfg.SkipPaths["/healthz"] = struct{}{}
	cfg.SkipPaths["/readyz"] = struct{}{}
	cfg.SkipPaths["/openapi.yaml"] = struct{}{}
	cfg.SkipPaths["/openapi.json"] = struct{}{}
	cfg.SkipPaths["/docs"] = struct{}{}
	cfg.SkipPaths["/docs/"] = struct{}{}

	spec, err := cfg.LoadSpec()
	if err != nil {
		panic(err)
	}
	router, err := legacyrouter.NewRouter(spec)
	if err != nil {
		panic(err)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !shouldValidateOpenAPIRequest(r.URL.Path, cfg.IncludePrefixes, cfg.SkipPaths) {
				next.ServeHTTP(w, r)
				return
			}

			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if !requestContentTypeAllowed(route, r) {
				writeOpenAPIValidationError(w, "content type does not match OpenAPI contract")
				return
			}
			input := &openapi3filter.RequestValidationInput{
				Request:    r,
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					AuthenticationFunc: func(context.Context, *openapi3filter.AuthenticationInput) error { return nil },
				},
			}
			if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
				writeOpenAPIValidationError(w, err.Error())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func shouldValidateOpenAPIRequest(path string, includePrefixes []string, skipPaths map[string]struct{}) bool {
	if _, ok := skipPaths[path]; ok {
		return false
	}
	for _, prefix := range includePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func requestContentTypeAllowed(route *routers.Route, r *http.Request) bool {
	if route == nil || route.Operation == nil || route.Operation.RequestBody == nil || route.Operation.RequestBody.Value == nil {
		return true
	}
	contentTypes := route.Operation.RequestBody.Value.Content
	if len(contentTypes) == 0 {
		return true
	}
	contentType := strings.TrimSpace(strings.ToLower(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if contentType == "" {
		return false
	}
	_, ok := contentTypes[contentType]
	return ok
}

func writeOpenAPIValidationError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(openAPIValidationErrorResponse{
		Error:   "invalid_request",
		Message: message,
		Details: map[string]any{"reason": message},
	})
}
