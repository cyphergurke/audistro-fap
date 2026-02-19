package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yourorg/fap/internal/security"
)

var (
	ErrInvalidAmount  = errors.New("invalid amount_msat")
	ErrInvalidAssetID = errors.New("invalid asset_id")
	ErrInvalidSubject = errors.New("invalid subject")
)

func validateCreateChallengeInput(assetID string, subject string, amountMSat int64, cfg ServiceConfig) error {
	if strings.TrimSpace(assetID) == "" {
		return ErrInvalidAssetID
	}
	if strings.TrimSpace(subject) == "" {
		return ErrInvalidSubject
	}
	if err := validateAmountMSat(amountMSat, cfg); err != nil {
		return err
	}
	return nil
}

func validateAmountMSat(amountMSat int64, cfg ServiceConfig) error {
	min := cfg.MinAmountMSat
	if min <= 0 {
		min = security.DefaultMinAmountMSat
	}

	max := cfg.MaxAmountMSat
	if max <= 0 {
		max = security.DefaultMaxAmountMSat
	}

	if amountMSat < min || amountMSat > max {
		return fmt.Errorf("%w: must be between %d and %d", ErrInvalidAmount, min, max)
	}
	return nil
}
