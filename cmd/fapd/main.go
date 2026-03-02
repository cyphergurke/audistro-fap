package main

import (
	"fmt"
	"os"

	"audistro-fap/internal/envcheck"
	"audistro-fap/pkg/fap"
)

func main() {
	envcheck.MustValidate()

	cfg, err := fap.LoadFromEnv()
	if err != nil {
		fmt.Println("config error:", err)
		os.Exit(1)
	}

	migrateOnly := false
	for _, arg := range os.Args[1:] {
		if arg == "-migrate" {
			migrateOnly = true
			break
		}
	}
	if migrateOnly {
		if err := runMigrations(cfg); err != nil {
			fmt.Println("migration error:", err)
			os.Exit(1)
		}
		fmt.Println("migrations completed")
		return
	}

	srv, err := fap.NewServer(cfg)
	if err != nil {
		fmt.Println("startup error:", err)
		os.Exit(1)
	}
	defer func() { _ = srv.Close() }()

	fmt.Println("fapd listening on", cfg.HTTPAddr)
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println("server error:", err)
		os.Exit(1)
	}
}
