package main

import (
	"context"
	"fmt"

	"audistro-fap/internal/store"
	"audistro-fap/pkg/fap"
)

func runMigrations(cfg fap.Config) error {
	repo, err := store.OpenSQLite(context.Background(), cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	return repo.Close()
}
