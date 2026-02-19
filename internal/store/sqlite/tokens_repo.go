package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yourorg/fap/internal/store/repo"
)

type TokensRepo struct {
	db *sql.DB
}

func NewTokensRepo(db *sql.DB) *TokensRepo {
	return &TokensRepo{db: db}
}

func (r *TokensRepo) Upsert(ctx context.Context, rec repo.TokenRecord) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert token tx: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO tokens (id, payment_hash, resource_id, token, issued_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(payment_hash, resource_id) DO UPDATE SET
			id = excluded.id,
			token = excluded.token,
			issued_at = excluded.issued_at,
			expires_at = excluded.expires_at`,
		rec.ID,
		rec.PaymentHash,
		rec.ResourceID,
		rec.Token,
		rec.IssuedAt,
		rec.ExpiresAt,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("upsert token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert token tx: %w", err)
	}

	return nil
}

func (r *TokensRepo) GetByPaymentHashResource(ctx context.Context, paymentHash string, resourceID string) (repo.TokenRecord, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, payment_hash, resource_id, token, issued_at, expires_at
		 FROM tokens
		 WHERE payment_hash = ? AND resource_id = ?`,
		paymentHash,
		resourceID,
	)

	var rec repo.TokenRecord
	if err := row.Scan(
		&rec.ID,
		&rec.PaymentHash,
		&rec.ResourceID,
		&rec.Token,
		&rec.IssuedAt,
		&rec.ExpiresAt,
	); err != nil {
		return repo.TokenRecord{}, fmt.Errorf("get token by payment hash and resource: %w", err)
	}

	return rec, nil
}
