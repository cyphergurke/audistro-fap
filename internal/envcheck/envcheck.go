package envcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type schema struct {
	Service string      `json:"service"`
	Keys    []schemaKey `json:"keys"`
}

type schemaKey struct {
	Key            string `json:"key"`
	RequiredInProd bool   `json:"required_in_prod"`
	Pattern        string `json:"pattern"`
	Redact         bool   `json:"redact"`
}

func MustValidate() {
	if err := Validate(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func Validate() error {
	mode := strings.TrimSpace(os.Getenv("AUDISTRO_ENV"))
	if mode == "" {
		return nil
	}
	if mode != "prod" && mode != "dev" && mode != "test" {
		return fmt.Errorf("envcheck: invalid env: AUDISTRO_ENV")
	}
	if mode != "prod" {
		return nil
	}

	cfg, err := loadSchema()
	if err != nil {
		return fmt.Errorf("envcheck: %w", err)
	}
	for _, key := range cfg.Keys {
		if !key.RequiredInProd {
			continue
		}
		value, ok := os.LookupEnv(key.Key)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("envcheck: missing required env: %s", key.Key)
		}
		matched, err := regexp.MatchString(key.Pattern, value)
		if err != nil {
			return fmt.Errorf("envcheck: invalid schema for env: %s", key.Key)
		}
		if err == nil && !matched {
			return fmt.Errorf("envcheck: invalid env: %s", key.Key)
		}
	}
	return nil
}

func loadSchema() (schema, error) {
	paths := []string{
		"ops/env.schema.json",
		filepath.Join("..", "..", "ops", "env.schema.json"),
	}
	if exePath, err := os.Executable(); err == nil {
		paths = append(paths, filepath.Join(filepath.Dir(exePath), "ops", "env.schema.json"))
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg schema
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return schema{}, fmt.Errorf("parse schema: %w", err)
		}
		return cfg, nil
	}
	return schema{}, errors.New("env schema not found")
}
