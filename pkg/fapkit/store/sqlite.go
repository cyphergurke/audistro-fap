package store

import "github.com/yourorg/fap/pkg/fapkit"

func NewSQLiteStore(dbPath string) (fapkit.Store, func() error, error) {
	return fapkit.NewSQLiteStore(dbPath)
}
