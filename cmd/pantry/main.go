package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/mwhite7112/woodpantry-pantry/internal/logging"
	"github.com/mwhite7112/woodpantry-pantry/internal/service"
)

func main() {
	logging.Setup()

	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := envOrDefault("PORT", "8080")

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		return fmt.Errorf("DB_URL is required")
	}

	dictURL := os.Getenv("DICTIONARY_URL")
	if dictURL == "" {
		return fmt.Errorf("DICTIONARY_URL is required")
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}

	extractModel := envOrDefault("EXTRACT_MODEL", "gpt-5-mini")

	sqlDB, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	if err := runMigrations(sqlDB); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	queries := db.New(sqlDB)
	httpClient := &http.Client{Timeout: 30 * time.Second}

	pantry := service.NewPantryService(queries)
	dict := clients.NewDictionaryClient(dictURL, httpClient)
	extractor := service.NewOpenAIExtractor(openaiKey, extractModel)
	ingest := service.NewIngestService(queries, dict, extractor)

	handler := api.NewRouter(pantry, ingest, dict)

	addr := fmt.Sprintf(":%s", port)
	slog.Info("pantry service listening", "addr", addr)
	if err := http.ListenAndServe(addr, handler); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
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
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
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
