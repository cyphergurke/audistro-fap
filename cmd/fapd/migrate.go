package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yourorg/fap/internal/store/sqlite"
)

func migrateDB(dbPath string) error {
	if err := ensureDBDir(dbPath); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	if err := sqlite.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate db: %w", err)
	}

	return nil
}

func ensureDBDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir == "." || dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create db directory %s: %w", dir, err)
	}
	return nil
}
