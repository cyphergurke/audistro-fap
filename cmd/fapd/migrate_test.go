package main

import (
	"path/filepath"
	"testing"

	"fap/pkg/fap"
)

func TestRunMigrations(t *testing.T) {
	cfg := fap.Config{DBPath: filepath.Join(t.TempDir(), "fap.sqlite")}
	if err := runMigrations(cfg); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
}
