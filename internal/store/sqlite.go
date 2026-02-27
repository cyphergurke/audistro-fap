package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

type SQLiteRepository struct {
	db *sql.DB
}

func OpenSQLite(ctx context.Context, dbPath string) (*SQLiteRepository, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, stmt := range pragmas {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("set sqlite pragma: %w", err)
		}
	}
	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Close() error { return r.db.Close() }

func (r *SQLiteRepository) DB() *sql.DB { return r.db }

func (r *SQLiteRepository) CreatePayee(ctx context.Context, p Payee) error {
	const q = `INSERT INTO payees (
		payee_id, display_name, rail, mode, lnbits_base_url,
		lnbits_invoice_key_enc, lnbits_read_key_enc, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		p.PayeeID, p.DisplayName, p.Rail, p.Mode, p.LNBitsBaseURL,
		p.LNBitsInvoiceKeyEnc, p.LNBitsReadKeyEnc, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create payee: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetByID(ctx context.Context, payeeID string) (Payee, error) {
	const q = `SELECT payee_id, display_name, rail, mode, lnbits_base_url, lnbits_invoice_key_enc, lnbits_read_key_enc, created_at, updated_at
		FROM payees WHERE payee_id = ?`
	var p Payee
	err := r.db.QueryRowContext(ctx, q, payeeID).Scan(
		&p.PayeeID, &p.DisplayName, &p.Rail, &p.Mode, &p.LNBitsBaseURL,
		&p.LNBitsInvoiceKeyEnc, &p.LNBitsReadKeyEnc, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Payee{}, ErrNotFound
		}
		return Payee{}, fmt.Errorf("get payee by id: %w", err)
	}
	return p, nil
}

func (r *SQLiteRepository) CreateAsset(ctx context.Context, a Asset) error {
	const q = `INSERT INTO assets (asset_id, payee_id, title, price_msat, resource_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, a.AssetID, a.PayeeID, a.Title, a.PriceMSat, a.ResourceID, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create asset: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetAssetByID(ctx context.Context, assetID string) (Asset, error) {
	const q = `SELECT asset_id, payee_id, title, price_msat, resource_id, created_at, updated_at FROM assets WHERE asset_id = ?`
	var a Asset
	err := r.db.QueryRowContext(ctx, q, assetID).Scan(&a.AssetID, &a.PayeeID, &a.Title, &a.PriceMSat, &a.ResourceID, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Asset{}, ErrNotFound
		}
		return Asset{}, fmt.Errorf("get asset by id: %w", err)
	}
	return a, nil
}

func (r *SQLiteRepository) CreateIntent(ctx context.Context, i PaymentIntent) error {
	const q = `INSERT INTO payment_intents (
		intent_id, asset_id, payee_id, amount_msat, bolt11, payment_hash, status, invoice_expires_at, settled_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		i.IntentID, i.AssetID, i.PayeeID, i.AmountMSat, i.Bolt11, i.PaymentHash,
		i.Status, i.InvoiceExpiresAt, i.SettledAt, i.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create intent: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetIntentByID(ctx context.Context, intentID string) (PaymentIntent, error) {
	const q = `SELECT intent_id, asset_id, payee_id, amount_msat, bolt11, payment_hash, status, invoice_expires_at, settled_at, created_at
		FROM payment_intents WHERE intent_id = ?`
	return r.scanIntent(ctx, q, intentID)
}

func (r *SQLiteRepository) GetIntentByPaymentHash(ctx context.Context, paymentHash string) (PaymentIntent, error) {
	const q = `SELECT intent_id, asset_id, payee_id, amount_msat, bolt11, payment_hash, status, invoice_expires_at, settled_at, created_at
		FROM payment_intents WHERE payment_hash = ?`
	return r.scanIntent(ctx, q, paymentHash)
}

func (r *SQLiteRepository) scanIntent(ctx context.Context, q string, arg string) (PaymentIntent, error) {
	var i PaymentIntent
	var settled sql.NullInt64
	err := r.db.QueryRowContext(ctx, q, arg).Scan(
		&i.IntentID, &i.AssetID, &i.PayeeID, &i.AmountMSat, &i.Bolt11, &i.PaymentHash,
		&i.Status, &i.InvoiceExpiresAt, &settled, &i.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return PaymentIntent{}, ErrNotFound
		}
		return PaymentIntent{}, fmt.Errorf("scan intent: %w", err)
	}
	if settled.Valid {
		v := settled.Int64
		i.SettledAt = &v
	}
	return i, nil
}

func (r *SQLiteRepository) MarkIntentSettled(ctx context.Context, intentID string, settledAt int64) error {
	const q = `UPDATE payment_intents SET status = 'settled', settled_at = ? WHERE intent_id = ? AND status != 'settled'`
	res, err := r.db.ExecContext(ctx, q, settledAt, intentID)
	if err != nil {
		return fmt.Errorf("mark settled: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		return nil
	}
	return nil
}

func (r *SQLiteRepository) CreateChallenge(ctx context.Context, c AccessChallenge) error {
	const q = `INSERT INTO challenges (
		challenge_id, device_id, asset_id, payee_id, amount_msat, memo, status, bolt11,
		lnbits_checking_id, lnbits_payment_hash, expires_at, paid_at, created_at, updated_at, idempotency_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	var idempotencyKey any
	if c.IdempotencyKey != nil && strings.TrimSpace(*c.IdempotencyKey) != "" {
		idempotencyKey = strings.TrimSpace(*c.IdempotencyKey)
	}
	_, err := r.db.ExecContext(ctx, q,
		c.ChallengeID, nullIfEmpty(c.DeviceID), c.AssetID, c.PayeeID, c.AmountMSat, c.Memo, c.Status, c.Bolt11,
		nullIfEmpty(c.LNBitsCheckingID), nullIfEmpty(c.LNBitsPaymentHash),
		c.ExpiresAt, c.PaidAt, c.CreatedAt, c.UpdatedAt, idempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("create challenge: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetChallengeByID(ctx context.Context, challengeID string) (AccessChallenge, error) {
	const q = `SELECT challenge_id, device_id, asset_id, payee_id, amount_msat, memo, status, bolt11,
		lnbits_checking_id, lnbits_payment_hash, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM challenges WHERE challenge_id = ?`
	return r.scanChallenge(ctx, q, challengeID)
}

func (r *SQLiteRepository) GetChallengeByIdempotencyKey(ctx context.Context, idempotencyKey string) (AccessChallenge, error) {
	const q = `SELECT challenge_id, device_id, asset_id, payee_id, amount_msat, memo, status, bolt11,
		lnbits_checking_id, lnbits_payment_hash, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM challenges WHERE idempotency_key = ?`
	return r.scanChallenge(ctx, q, idempotencyKey)
}

func (r *SQLiteRepository) GetChallengeByLNBitsPaymentHash(ctx context.Context, paymentHash string) (AccessChallenge, error) {
	const q = `SELECT challenge_id, device_id, asset_id, payee_id, amount_msat, memo, status, bolt11,
		lnbits_checking_id, lnbits_payment_hash, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM challenges WHERE lnbits_payment_hash = ?`
	return r.scanChallenge(ctx, q, paymentHash)
}

func (r *SQLiteRepository) GetChallengeByLNBitsCheckingID(ctx context.Context, checkingID string) (AccessChallenge, error) {
	const q = `SELECT challenge_id, device_id, asset_id, payee_id, amount_msat, memo, status, bolt11,
		lnbits_checking_id, lnbits_payment_hash, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM challenges WHERE lnbits_checking_id = ?`
	return r.scanChallenge(ctx, q, checkingID)
}

func (r *SQLiteRepository) scanChallenge(ctx context.Context, q string, arg string) (AccessChallenge, error) {
	var c AccessChallenge
	var paidAt sql.NullInt64
	var memo sql.NullString
	var deviceID sql.NullString
	var checkingID sql.NullString
	var paymentHash sql.NullString
	var idempotencyKey sql.NullString
	err := r.db.QueryRowContext(ctx, q, arg).Scan(
		&c.ChallengeID, &deviceID, &c.AssetID, &c.PayeeID, &c.AmountMSat, &memo, &c.Status, &c.Bolt11,
		&checkingID, &paymentHash, &c.ExpiresAt, &paidAt, &c.CreatedAt, &c.UpdatedAt, &idempotencyKey,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return AccessChallenge{}, ErrNotFound
		}
		return AccessChallenge{}, fmt.Errorf("scan challenge: %w", err)
	}
	c.Memo = nullableStringValue(memo)
	c.DeviceID = nullableStringValue(deviceID)
	c.LNBitsCheckingID = nullableStringValue(checkingID)
	c.LNBitsPaymentHash = nullableStringValue(paymentHash)
	if paidAt.Valid {
		value := paidAt.Int64
		c.PaidAt = &value
	}
	if idempotencyKey.Valid {
		value := idempotencyKey.String
		c.IdempotencyKey = &value
	}
	return c, nil
}

func (r *SQLiteRepository) UpdateChallengeStatus(ctx context.Context, challengeID string, status string, paidAt *int64, updatedAt int64) error {
	const q = `UPDATE challenges SET status = ?, paid_at = ?, updated_at = ? WHERE challenge_id = ?`
	res, err := r.db.ExecContext(ctx, q, status, paidAt, updatedAt, challengeID)
	if err != nil {
		return fmt.Errorf("update challenge status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("challenge rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) CreateDevice(ctx context.Context, d Device) error {
	const q = `INSERT INTO devices (device_id, created_at, last_seen_at) VALUES (?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q, d.DeviceID, d.CreatedAt, d.LastSeenAt)
	if err != nil {
		return fmt.Errorf("create device: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetDeviceByID(ctx context.Context, deviceID string) (Device, error) {
	const q = `SELECT device_id, created_at, last_seen_at FROM devices WHERE device_id = ?`
	var d Device
	err := r.db.QueryRowContext(ctx, q, deviceID).Scan(&d.DeviceID, &d.CreatedAt, &d.LastSeenAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return Device{}, ErrNotFound
		}
		return Device{}, fmt.Errorf("get device by id: %w", err)
	}
	return d, nil
}

func (r *SQLiteRepository) TouchDevice(ctx context.Context, deviceID string, lastSeenAt int64) error {
	const q = `UPDATE devices SET last_seen_at = ? WHERE device_id = ?`
	res, err := r.db.ExecContext(ctx, q, lastSeenAt, deviceID)
	if err != nil {
		return fmt.Errorf("touch device: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch device rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) CreateAccessGrant(ctx context.Context, g AccessGrant) error {
	const q = `INSERT INTO access_grants (
		grant_id, device_id, asset_id, scope, minutes_purchased, valid_from, valid_until,
		status, challenge_id, amount_msat, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		g.GrantID,
		g.DeviceID,
		g.AssetID,
		g.Scope,
		g.MinutesPurchased,
		g.ValidFrom,
		g.ValidUntil,
		g.Status,
		g.ChallengeID,
		g.AmountMSat,
		g.CreatedAt,
		g.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create access grant: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetAccessGrantByChallengeID(ctx context.Context, challengeID string) (AccessGrant, error) {
	const q = `SELECT grant_id, device_id, asset_id, scope, minutes_purchased, valid_from, valid_until,
		status, challenge_id, amount_msat, created_at, updated_at
		FROM access_grants WHERE challenge_id = ? ORDER BY created_at DESC LIMIT 1`
	return r.scanAccessGrant(ctx, q, challengeID)
}

func (r *SQLiteRepository) GetLatestAccessGrantByDeviceAsset(ctx context.Context, deviceID string, assetID string) (AccessGrant, error) {
	const q = `SELECT grant_id, device_id, asset_id, scope, minutes_purchased, valid_from, valid_until,
		status, challenge_id, amount_msat, created_at, updated_at
		FROM access_grants
		WHERE device_id = ? AND asset_id = ?
		ORDER BY updated_at DESC, created_at DESC
		LIMIT 1`
	var g AccessGrant
	var validFrom sql.NullInt64
	var validUntil sql.NullInt64
	err := r.db.QueryRowContext(ctx, q, deviceID, assetID).Scan(
		&g.GrantID,
		&g.DeviceID,
		&g.AssetID,
		&g.Scope,
		&g.MinutesPurchased,
		&validFrom,
		&validUntil,
		&g.Status,
		&g.ChallengeID,
		&g.AmountMSat,
		&g.CreatedAt,
		&g.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return AccessGrant{}, ErrNotFound
		}
		return AccessGrant{}, fmt.Errorf("get latest access grant by device asset: %w", err)
	}
	if validFrom.Valid {
		value := validFrom.Int64
		g.ValidFrom = &value
	}
	if validUntil.Valid {
		value := validUntil.Int64
		g.ValidUntil = &value
	}
	return g, nil
}

func (r *SQLiteRepository) ActivateAccessGrant(ctx context.Context, grantID string, validFrom int64, validUntil int64, updatedAt int64) error {
	const q = `UPDATE access_grants
		SET valid_from = COALESCE(valid_from, ?),
		    valid_until = COALESCE(valid_until, ?),
		    updated_at = ?,
		    status = CASE WHEN status = 'revoked' THEN 'revoked' ELSE status END
		WHERE grant_id = ?`
	res, err := r.db.ExecContext(ctx, q, validFrom, validUntil, updatedAt, grantID)
	if err != nil {
		return fmt.Errorf("activate access grant: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("activate access grant rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) UpdateAccessGrantStatus(ctx context.Context, grantID string, status string, updatedAt int64) error {
	const q = `UPDATE access_grants SET status = ?, updated_at = ? WHERE grant_id = ?`
	res, err := r.db.ExecContext(ctx, q, status, updatedAt, grantID)
	if err != nil {
		return fmt.Errorf("update access grant status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update access grant status rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) ListAccessGrantsByDevice(ctx context.Context, deviceID string, assetID string) ([]AccessGrant, error) {
	query := `SELECT grant_id, device_id, asset_id, scope, minutes_purchased, valid_from, valid_until,
		status, challenge_id, amount_msat, created_at, updated_at
		FROM access_grants
		WHERE device_id = ?`
	args := []any{deviceID}
	if strings.TrimSpace(assetID) != "" {
		query += " AND asset_id = ?"
		args = append(args, assetID)
	}
	query += " ORDER BY updated_at DESC, created_at DESC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list access grants by device: %w", err)
	}
	defer rows.Close()

	items := make([]AccessGrant, 0, 8)
	for rows.Next() {
		var g AccessGrant
		var validFrom sql.NullInt64
		var validUntil sql.NullInt64
		if err := rows.Scan(
			&g.GrantID,
			&g.DeviceID,
			&g.AssetID,
			&g.Scope,
			&g.MinutesPurchased,
			&validFrom,
			&validUntil,
			&g.Status,
			&g.ChallengeID,
			&g.AmountMSat,
			&g.CreatedAt,
			&g.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan access grant row: %w", err)
		}
		if validFrom.Valid {
			value := validFrom.Int64
			g.ValidFrom = &value
		}
		if validUntil.Valid {
			value := validUntil.Int64
			g.ValidUntil = &value
		}
		items = append(items, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate access grant rows: %w", err)
	}
	return items, nil
}

func (r *SQLiteRepository) scanAccessGrant(ctx context.Context, q string, arg string) (AccessGrant, error) {
	var g AccessGrant
	var validFrom sql.NullInt64
	var validUntil sql.NullInt64
	err := r.db.QueryRowContext(ctx, q, arg).Scan(
		&g.GrantID,
		&g.DeviceID,
		&g.AssetID,
		&g.Scope,
		&g.MinutesPurchased,
		&validFrom,
		&validUntil,
		&g.Status,
		&g.ChallengeID,
		&g.AmountMSat,
		&g.CreatedAt,
		&g.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return AccessGrant{}, ErrNotFound
		}
		return AccessGrant{}, fmt.Errorf("scan access grant: %w", err)
	}
	if validFrom.Valid {
		value := validFrom.Int64
		g.ValidFrom = &value
	}
	if validUntil.Valid {
		value := validUntil.Int64
		g.ValidUntil = &value
	}
	return g, nil
}

func (r *SQLiteRepository) CreateAccessToken(ctx context.Context, t AccessToken) error {
	const q = `INSERT INTO access_tokens (
		token_id, intent_id, payee_id, asset_id, resource_id, subject, token, expires_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		t.TokenID, t.IntentID, t.PayeeID, t.AssetID, t.ResourceID, t.Subject, t.Token, t.ExpiresAt, t.CreatedAt,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return err
		}
		return fmt.Errorf("create access token: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetAccessTokenByIntentID(ctx context.Context, intentID string) (AccessToken, error) {
	const q = `SELECT token_id, intent_id, payee_id, asset_id, resource_id, subject, token, expires_at, created_at
		FROM access_tokens WHERE intent_id = ?`
	var t AccessToken
	err := r.db.QueryRowContext(ctx, q, intentID).Scan(
		&t.TokenID, &t.IntentID, &t.PayeeID, &t.AssetID, &t.ResourceID,
		&t.Subject, &t.Token, &t.ExpiresAt, &t.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return AccessToken{}, ErrNotFound
		}
		return AccessToken{}, fmt.Errorf("get token by intent id: %w", err)
	}
	return t, nil
}

func (r *SQLiteRepository) UpdateAccessTokenByIntentID(ctx context.Context, t AccessToken) error {
	const q = `UPDATE access_tokens
		SET token_id = ?, payee_id = ?, asset_id = ?, resource_id = ?, subject = ?, token = ?, expires_at = ?, created_at = ?
		WHERE intent_id = ?`
	res, err := r.db.ExecContext(ctx, q,
		t.TokenID, t.PayeeID, t.AssetID, t.ResourceID, t.Subject, t.Token, t.ExpiresAt, t.CreatedAt, t.IntentID,
	)
	if err != nil {
		return fmt.Errorf("update access token by intent id: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("access token rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) CreateBoost(ctx context.Context, b Boost) error {
	const q = `INSERT INTO boosts (
		boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(ctx, q,
		b.BoostID, nullIfEmpty(b.DeviceID), b.AssetID, b.PayeeID, b.AmountMSat, b.Bolt11, b.LNBitsPaymentHash, b.LNBitsCheckingID, b.LNBitsWebhookEventID,
		b.Status, b.ExpiresAt, b.PaidAt, b.CreatedAt, b.UpdatedAt, b.IdempotencyKey,
	)
	if err != nil {
		return fmt.Errorf("create boost: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) GetBoostByID(ctx context.Context, boostID string) (Boost, error) {
	const q = `SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM boosts WHERE boost_id = ?`
	return r.scanBoost(ctx, q, boostID)
}

func (r *SQLiteRepository) GetBoostByIdempotencyKey(ctx context.Context, idempotencyKey string) (Boost, error) {
	const q = `SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM boosts WHERE idempotency_key = ?`
	return r.scanBoost(ctx, q, idempotencyKey)
}

func (r *SQLiteRepository) GetBoostByLNBitsPaymentHash(ctx context.Context, paymentHash string) (Boost, error) {
	const q = `SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM boosts WHERE lnbits_payment_hash = ?`
	return r.scanBoost(ctx, q, paymentHash)
}

func (r *SQLiteRepository) GetBoostByLNBitsCheckingID(ctx context.Context, checkingID string) (Boost, error) {
	const q = `SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM boosts WHERE lnbits_checking_id = ?`
	return r.scanBoost(ctx, q, checkingID)
}

func (r *SQLiteRepository) GetBoostByLNBitsWebhookEventID(ctx context.Context, eventID string) (Boost, error) {
	const q = `SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id,
		status, expires_at, paid_at, created_at, updated_at, idempotency_key
		FROM boosts WHERE lnbits_webhook_event_id = ?`
	return r.scanBoost(ctx, q, eventID)
}

func (r *SQLiteRepository) scanBoost(ctx context.Context, q string, arg string) (Boost, error) {
	var b Boost
	var paidAt sql.NullInt64
	var deviceID sql.NullString
	var lnbitsPaymentHash sql.NullString
	var lnbitsCheckingID sql.NullString
	var lnbitsWebhookEventID sql.NullString
	err := r.db.QueryRowContext(ctx, q, arg).Scan(
		&b.BoostID, &deviceID, &b.AssetID, &b.PayeeID, &b.AmountMSat, &b.Bolt11, &lnbitsPaymentHash, &lnbitsCheckingID, &lnbitsWebhookEventID,
		&b.Status, &b.ExpiresAt, &paidAt, &b.CreatedAt, &b.UpdatedAt, &b.IdempotencyKey,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Boost{}, ErrNotFound
		}
		return Boost{}, fmt.Errorf("scan boost: %w", err)
	}
	if paidAt.Valid {
		value := paidAt.Int64
		b.PaidAt = &value
	}
	b.DeviceID = nullableStringValue(deviceID)
	b.LNBitsPaymentHash = nullableStringValue(lnbitsPaymentHash)
	b.LNBitsCheckingID = nullableStringValue(lnbitsCheckingID)
	b.LNBitsWebhookEventID = nullableStringValue(lnbitsWebhookEventID)
	return b, nil
}

func (r *SQLiteRepository) UpdateBoostStatus(ctx context.Context, boostID string, status string, paidAt *int64, updatedAt int64) error {
	const q = `UPDATE boosts SET status = ?, paid_at = ?, updated_at = ? WHERE boost_id = ?`
	res, err := r.db.ExecContext(ctx, q, status, paidAt, updatedAt, boostID)
	if err != nil {
		return fmt.Errorf("update boost status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("boost rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) UpdateBoostLNBitsWebhookEventID(ctx context.Context, boostID string, eventID string, updatedAt int64) error {
	const q = `UPDATE boosts SET lnbits_webhook_event_id = ?, updated_at = ? WHERE boost_id = ?`
	res, err := r.db.ExecContext(ctx, q, eventID, updatedAt, boostID)
	if err != nil {
		return fmt.Errorf("update boost webhook event id: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("boost rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) RecordWebhookEvent(ctx context.Context, event WebhookEvent) (bool, error) {
	const q = `INSERT OR IGNORE INTO webhook_events (event_key, received_at) VALUES (?, ?)`
	res, err := r.db.ExecContext(ctx, q, event.EventKey, event.ReceivedAt)
	if err != nil {
		return false, fmt.Errorf("record webhook event: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("record webhook event rows affected: %w", err)
	}
	return affected > 0, nil
}

func (r *SQLiteRepository) PruneWebhookEvents(ctx context.Context, olderThanUnix int64) (int64, error) {
	const q = `DELETE FROM webhook_events WHERE received_at < ?`
	res, err := r.db.ExecContext(ctx, q, olderThanUnix)
	if err != nil {
		return 0, fmt.Errorf("prune webhook events: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune webhook events rows affected: %w", err)
	}
	return affected, nil
}

func (r *SQLiteRepository) ListBoosts(ctx context.Context, params ListBoostsParams) ([]Boost, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}

	query := strings.Builder{}
	query.WriteString(`SELECT boost_id, device_id, asset_id, payee_id, amount_msat, bolt11, lnbits_payment_hash, lnbits_checking_id, lnbits_webhook_event_id, status, expires_at, paid_at, created_at, updated_at, idempotency_key FROM boosts`)

	conditions := make([]string, 0, 4)
	args := make([]any, 0, 8)
	if params.AssetID != "" {
		conditions = append(conditions, "asset_id = ?")
		args = append(args, params.AssetID)
	}
	if params.PayeeID != "" {
		conditions = append(conditions, "payee_id = ?")
		args = append(args, params.PayeeID)
	}
	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, params.Status)
	}
	if params.Cursor != nil {
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND boost_id < ?))")
		args = append(args, params.Cursor.CreatedAt, params.Cursor.CreatedAt, params.Cursor.BoostID)
	}
	if len(conditions) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(conditions, " AND "))
	}
	query.WriteString(" ORDER BY created_at DESC, boost_id DESC LIMIT ?")
	args = append(args, params.Limit)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list boosts (limit=%s): %w", strconv.Itoa(params.Limit), err)
	}
	defer rows.Close()

	items := make([]Boost, 0, params.Limit)
	for rows.Next() {
		var value Boost
		var paidAt sql.NullInt64
		var deviceID sql.NullString
		var lnbitsPaymentHash sql.NullString
		var lnbitsCheckingID sql.NullString
		var lnbitsWebhookEventID sql.NullString
		if err := rows.Scan(
			&value.BoostID,
			&deviceID,
			&value.AssetID,
			&value.PayeeID,
			&value.AmountMSat,
			&value.Bolt11,
			&lnbitsPaymentHash,
			&lnbitsCheckingID,
			&lnbitsWebhookEventID,
			&value.Status,
			&value.ExpiresAt,
			&paidAt,
			&value.CreatedAt,
			&value.UpdatedAt,
			&value.IdempotencyKey,
		); err != nil {
			return nil, fmt.Errorf("scan boost row: %w", err)
		}
		if paidAt.Valid {
			paid := paidAt.Int64
			value.PaidAt = &paid
		}
		value.DeviceID = nullableStringValue(deviceID)
		value.LNBitsPaymentHash = nullableStringValue(lnbitsPaymentHash)
		value.LNBitsCheckingID = nullableStringValue(lnbitsCheckingID)
		value.LNBitsWebhookEventID = nullableStringValue(lnbitsWebhookEventID)
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate boosts rows: %w", err)
	}
	return items, nil
}

func (r *SQLiteRepository) InsertLedgerEntryIfNotExists(ctx context.Context, entry LedgerEntry) error {
	const q = `INSERT OR IGNORE INTO ledger_entries (
		entry_id, device_id, kind, status, asset_id, payee_id, amount_msat, currency,
		related_id, reference_id, memo, created_at, updated_at, paid_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := r.db.ExecContext(
		ctx,
		q,
		entry.EntryID,
		entry.DeviceID,
		entry.Kind,
		entry.Status,
		nullIfEmpty(entry.AssetID),
		entry.PayeeID,
		entry.AmountMSat,
		entry.Currency,
		entry.RelatedID,
		nullIfEmpty(entry.ReferenceID),
		nullIfEmpty(entry.Memo),
		entry.CreatedAt,
		entry.UpdatedAt,
		entry.PaidAt,
	)
	if err != nil {
		return fmt.Errorf("insert ledger entry: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) UpdateLedgerStatus(ctx context.Context, kind string, relatedID string, status string, paidAt *int64, referenceID string, updatedAt int64) error {
	const q = `UPDATE ledger_entries
		SET status = ?, paid_at = ?, updated_at = ?, reference_id = CASE
			WHEN (reference_id IS NULL OR reference_id = '') AND ? != '' THEN ?
			ELSE reference_id
		END
		WHERE kind = ? AND related_id = ?`
	res, err := r.db.ExecContext(ctx, q, status, paidAt, updatedAt, strings.TrimSpace(referenceID), strings.TrimSpace(referenceID), kind, relatedID)
	if err != nil {
		return fmt.Errorf("update ledger status: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update ledger status rows affected: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *SQLiteRepository) ListLedgerEntriesForDevice(ctx context.Context, params ListLedgerEntriesParams) ([]LedgerEntry, error) {
	if params.Limit <= 0 {
		params.Limit = 20
	}

	query := strings.Builder{}
	query.WriteString(`SELECT entry_id, device_id, kind, status, asset_id, payee_id, amount_msat, currency, related_id, reference_id, memo, created_at, updated_at, paid_at
		FROM ledger_entries`)

	conditions := make([]string, 0, 6)
	args := make([]any, 0, 10)
	conditions = append(conditions, "device_id = ?")
	args = append(args, params.DeviceID)

	if params.Kind != "" {
		conditions = append(conditions, "kind = ?")
		args = append(args, params.Kind)
	}
	if params.Status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, params.Status)
	}
	if params.AssetID != "" {
		conditions = append(conditions, "asset_id = ?")
		args = append(args, params.AssetID)
	}
	if params.Cursor != nil {
		conditions = append(conditions, "(created_at < ? OR (created_at = ? AND entry_id < ?))")
		args = append(args, params.Cursor.CreatedAt, params.Cursor.CreatedAt, params.Cursor.EntryID)
	}
	if len(conditions) > 0 {
		query.WriteString(" WHERE ")
		query.WriteString(strings.Join(conditions, " AND "))
	}
	query.WriteString(" ORDER BY created_at DESC, entry_id DESC LIMIT ?")
	args = append(args, params.Limit)

	rows, err := r.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list ledger entries (limit=%s): %w", strconv.Itoa(params.Limit), err)
	}
	defer rows.Close()

	items := make([]LedgerEntry, 0, params.Limit)
	for rows.Next() {
		var value LedgerEntry
		var assetID sql.NullString
		var referenceID sql.NullString
		var memo sql.NullString
		var paidAt sql.NullInt64
		if err := rows.Scan(
			&value.EntryID,
			&value.DeviceID,
			&value.Kind,
			&value.Status,
			&assetID,
			&value.PayeeID,
			&value.AmountMSat,
			&value.Currency,
			&value.RelatedID,
			&referenceID,
			&memo,
			&value.CreatedAt,
			&value.UpdatedAt,
			&paidAt,
		); err != nil {
			return nil, fmt.Errorf("scan ledger row: %w", err)
		}
		value.AssetID = nullableStringValue(assetID)
		value.ReferenceID = nullableStringValue(referenceID)
		value.Memo = nullableStringValue(memo)
		if paidAt.Valid {
			paid := paidAt.Int64
			value.PaidAt = &paid
		}
		items = append(items, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger rows: %w", err)
	}
	return items, nil
}

func nullableStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func nullIfEmpty(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

var _ Repository = (*SQLiteRepository)(nil)
