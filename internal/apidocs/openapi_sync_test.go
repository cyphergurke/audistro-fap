package apidocs

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEmbeddedSpecMatchesCanonicalSpec(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate current file")
	}
	canonicalPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "api", "openapi.v1.yaml")
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		t.Fatalf("read canonical spec: %v", err)
	}
	if !bytes.Equal(canonical, openAPIYAML) {
		t.Fatalf("embedded spec is out of sync with %s", canonicalPath)
	}

	compatPath := filepath.Join(filepath.Dir(currentFile), "..", "api", "openapi", "openapi.yaml")
	compat, err := os.ReadFile(compatPath)
	if err != nil {
		t.Fatalf("read compatibility spec: %v", err)
	}
	if !bytes.Equal(canonical, compat) {
		t.Fatalf("compatibility spec is out of sync with %s", canonicalPath)
	}
}
