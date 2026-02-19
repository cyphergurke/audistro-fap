package hls

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrNotFound = errors.New("hls key not found")

type Repository interface {
	GetKey(ctx context.Context, assetID string) ([]byte, error)
}

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) GetKey(ctx context.Context, assetID string) ([]byte, error) {
	var keyHex string
	err := r.db.QueryRowContext(
		ctx,
		`SELECT key_hex FROM hls_keys WHERE asset_id = ?`,
		assetID,
	).Scan(&keyHex)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get hls key: %w", err)
	}

	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("decode hls key hex: %w", err)
	}
	if len(keyBytes) != 16 {
		return nil, fmt.Errorf("invalid hls key length: got %d want 16", len(keyBytes))
	}

	return keyBytes, nil
}
