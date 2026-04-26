package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"mtproxy-control/apps/api/internal/database"
	"mtproxy-control/apps/api/internal/migrations"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] != "up" {
		fmt.Fprintf(os.Stderr, "usage: migrate [up]\n")
		os.Exit(2)
	}

	path := env("DATABASE_PATH", "./data/panel.db")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatalf("create database directory: %v", err)
	}

	db, err := database.Open(path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	if err := migrations.Up(context.Background(), db); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}

	fmt.Printf("migrations applied: %s\n", path)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
