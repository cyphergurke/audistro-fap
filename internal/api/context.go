package api

import (
	"context"

	"fap/internal/service"
)

type masterKeyContextKey struct{}
type encryptorContextKey struct{}

type EncryptFunc = service.EncryptFunc

func WithMasterKey(ctx context.Context, key []byte) context.Context {
	return context.WithValue(ctx, masterKeyContextKey{}, key)
}

func GetMasterKey(ctx context.Context) []byte {
	v, _ := ctx.Value(masterKeyContextKey{}).([]byte)
	return v
}

func WithEncryptor(ctx context.Context, fn EncryptFunc) context.Context {
	return context.WithValue(ctx, encryptorContextKey{}, fn)
}

func GetEncryptor(ctx context.Context) EncryptFunc {
	v, _ := ctx.Value(encryptorContextKey{}).(EncryptFunc)
	return v
}
