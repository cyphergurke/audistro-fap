package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"audistro-fap/internal/apidocs"
)

type coveredEndpoint struct {
	Method string
	Path   string
}

func TestOpenAPISpecCoversRegisteredEndpoints(t *testing.T) {
	registered, err := registeredEndpoints()
	if err != nil {
		t.Fatalf("read registered endpoints: %v", err)
	}
	operations, err := documentedOperations()
	if err != nil {
		t.Fatalf("load documented operations: %v", err)
	}

	missing := make([]string, 0)
	for _, ep := range registered {
		methods := operations[normalizeCoveragePath(ep.Path)]
		if methods == nil || !methods[ep.Method] {
			missing = append(missing, fmt.Sprintf("%s %s", ep.Method, ep.Path))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("openapi spec is missing endpoint(s):\n%s", strings.Join(missing, "\n"))
	}
}

func documentedOperations() (map[string]map[string]bool, error) {
	spec, err := apidocs.LoadSpec()
	if err != nil {
		return nil, err
	}
	ops := make(map[string]map[string]bool, len(spec.Paths.Map()))
	for path, item := range spec.Paths.Map() {
		normalized := normalizeCoveragePath(path)
		methods := make(map[string]bool)
		if item.Get != nil {
			methods["GET"] = true
		}
		if item.Post != nil {
			methods["POST"] = true
		}
		if item.Put != nil {
			methods["PUT"] = true
		}
		if item.Patch != nil {
			methods["PATCH"] = true
		}
		if item.Delete != nil {
			methods["DELETE"] = true
		}
		if item.Head != nil {
			methods["HEAD"] = true
		}
		if len(methods) > 0 {
			ops[normalized] = methods
		}
	}
	return ops, nil
}

func registeredEndpoints() ([]coveredEndpoint, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("locate current file")
	}
	httpFile := filepath.Join(filepath.Dir(currentFile), "http.go")
	content, err := os.ReadFile(httpFile)
	if err != nil {
		return nil, err
	}
	matches := regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)"`).FindAllStringSubmatch(string(content), -1)
	found := make(map[coveredEndpoint]struct{}, len(matches))
	for _, m := range matches {
		if len(m) != 3 {
			continue
		}
		path := normalizeCoveragePath(m[2])
		if path == "/healthz" || path == "/readyz" {
			continue
		}
		found[coveredEndpoint{Method: m[1], Path: path}] = struct{}{}
	}
	out := make([]coveredEndpoint, 0, len(found))
	for ep := range found {
		out = append(out, ep)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func normalizeCoveragePath(path string) string {
	if path == "/" {
		return path
	}
	return strings.TrimSuffix(path, "/")
}
