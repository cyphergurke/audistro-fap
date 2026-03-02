package api

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

const (
	openAPIYAMLPath = "/openapi.yaml"
	openAPIJSONPath = "/openapi.json"
)

var (
	//go:embed openapi/openapi.yaml
	openAPIYAML []byte

	loadOpenAPISpecOnce sync.Once
	loadedOpenAPISpec   *openapi3.T
	loadedOpenAPIJSON   []byte
	loadOpenAPISpecErr  error
)

func loadOpenAPISpec() (*openapi3.T, error) {
	loadOpenAPISpecOnce.Do(func() {
		loader := openapi3.NewLoader()
		spec, err := loader.LoadFromData(openAPIYAML)
		if err != nil {
			loadOpenAPISpecErr = err
			return
		}
		if err := spec.Validate(loader.Context); err != nil {
			loadOpenAPISpecErr = err
			return
		}
		jsonSpec, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			loadOpenAPISpecErr = err
			return
		}
		loadedOpenAPISpec = spec
		loadedOpenAPIJSON = jsonSpec
	})
	return loadedOpenAPISpec, loadOpenAPISpecErr
}

const scalarHTML = `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>audistro-fap API Docs</title>
  </head>
  <body>
    <script id="api-reference" data-url="/openapi.json"></script>
    <script src="https://cdn.jsdelivr.net/npm/@scalar/api-reference"></script>
  </body>
</html>`

func openAPIYAMLHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openAPIYAML)
}

func openAPIJSONHandler(w http.ResponseWriter, _ *http.Request) {
	_, err := loadOpenAPISpec()
	if err != nil {
		http.Error(w, fmt.Sprintf("load openapi spec: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(loadedOpenAPIJSON)
}

func docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(scalarHTML))
}
