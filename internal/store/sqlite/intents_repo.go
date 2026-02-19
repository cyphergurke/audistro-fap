package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yourorg/fap/internal/store/repo"
)

type IntentsRepo struct {
	db *sql.DB
}

func NewIntentsRepo(db *sql.DB) *IntentsRepo {
	return &IntentsRepo{db: db}
}

func (r *IntentsRepo) CreatePending(ctx context.Context, intent repo.PaymentIntent) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create payment intent tx: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO payment_intents (
			id, resource_id, asset_id, subject, amount_msat, bolt11, payment_hash,
			status, created_at, expires_at, settled_at,
			rail, amount, amount_unit, asset, provider_ref, offer
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		intent.ID,
		intent.ResourceID,
		intent.AssetID,
		intent.Subject,
		intent.AmountMSat,
		intent.Bolt11,
		intent.PaymentHash,
		intent.Status,
		intent.CreatedAt,
		intent.ExpiresAt,
		intent.SettledAt,
		string(intent.Rail),
		intent.Amount,
		string(intent.AmountUnit),
		intent.Asset,
		intent.ProviderRef,
		intent.Offer,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert payment intent: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create payment intent tx: %w", err)
	}

	return nil
}

func (r *IntentsRepo) GetByID(ctx context.Context, intentID string) (repo.PaymentIntent, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, resource_id, asset_id, subject, amount_msat, bolt11, payment_hash, status, created_at, expires_at, settled_at,
		        rail, amount, amount_unit, asset, provider_ref, offer
		 FROM payment_intents WHERE id = ?`,
		intentID,
	)
	intent, err := scanIntent(row)
	if err != nil {
		return repo.PaymentIntent{}, err
	}
	return intent, nil
}

func (r *IntentsRepo) GetByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string) (repo.PaymentIntent, error) {
	row := r.db.QueryRowContext(
		ctx,
		`SELECT id, resource_id, asset_id, subject, amount_msat, bolt11, payment_hash, status, created_at, expires_at, settled_at,
		        rail, amount, amount_unit, asset, provider_ref, offer
		 FROM payment_intents WHERE rail = ? AND provider_ref = ?`,
		string(rail),
		providerRef,
	)
	intent, err := scanIntent(row)
	if err == nil {
		return intent, nil
	}

	// Backward compatibility path for pre-migration records.
	row = r.db.QueryRowContext(
		ctx,
		`SELECT id, resource_id, asset_id, subject, amount_msat, bolt11, payment_hash, status, created_at, expires_at, settled_at,
		        rail, amount, amount_unit, asset, provider_ref, offer
		 FROM payment_intents WHERE payment_hash = ?`,
		providerRef,
	)
	return scanIntent(row)
}

func (r *IntentsRepo) MarkSettledByProviderRef(ctx context.Context, rail repo.PaymentRail, providerRef string, settledAt int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin mark settled tx: %w", err)
	}

	res, err := tx.ExecContext(
		ctx,
		`UPDATE payment_intents
		 SET status = 'settled', settled_at = ?
		 WHERE (rail = ? AND provider_ref = ?) OR payment_hash = ?`,
		settledAt,
		string(rail),
		providerRef,
		providerRef,
	)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("update payment intent settled: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("rows affected mark settled: %w", err)
	}
	if affected == 0 {
		_ = tx.Rollback()
		return sql.ErrNoRows
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark settled tx: %w", err)
	}
	return nil
}

func scanIntent(row *sql.Row) (repo.PaymentIntent, error) {
	var intent repo.PaymentIntent
	var rail string
	var amountUnit string

	err := row.Scan(
		&intent.ID,
		&intent.ResourceID,
		&intent.AssetID,
		&intent.Subject,
		&intent.AmountMSat,
		&intent.Bolt11,
		&intent.PaymentHash,
		&intent.Status,
		&intent.CreatedAt,
		&intent.ExpiresAt,
		&intent.SettledAt,
		&rail,
		&intent.Amount,
		&amountUnit,
		&intent.Asset,
		&intent.ProviderRef,
		&intent.Offer,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return repo.PaymentIntent{}, sql.ErrNoRows
		}
		return repo.PaymentIntent{}, fmt.Errorf("scan payment intent: %w", err)
	}

	intent.Rail = repo.PaymentRail(rail)
	intent.AmountUnit = repo.AmountUnit(amountUnit)
	return intent, nil
}
