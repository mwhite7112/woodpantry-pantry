package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/mwhite7112/woodpantry-pantry/internal/api"
	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

func main() {
	port := envOrDefault("PORT", "8080")

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		log.Fatal("DB_URL is required")
	}

	dictURL := os.Getenv("DICTIONARY_URL")
	if dictURL == "" {
		log.Fatal("DICTIONARY_URL is required")
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	extractModel := envOrDefault("EXTRACT_MODEL", "gpt-4o-mini")

	sqlDB, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	if err := runMigrations(sqlDB); err != nil {
		log.Fatalf("migrations: %v", err)
	}

	queries := db.New(sqlDB)
	httpClient := &http.Client{Timeout: 30 * time.Second}

	pantry := service.NewPantryService(queries)
	dict := clients.NewDictionaryClient(dictURL, httpClient)
	ingest := service.NewIngestService(queries, dict, openaiKey, extractModel)

	handler := api.NewRouter(pantry, ingest, dict)

	addr := fmt.Sprintf(":%s", port)
	log.Printf("pantry service listening on %s", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func runMigrations(sqlDB *sql.DB) error {
	srcDriver, err := iofs.New(db.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration source: %w", err)
	}
	dbDriver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create migration driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", srcDriver, "postgres", dbDriver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
