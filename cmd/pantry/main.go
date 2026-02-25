package main

import (
	"context"
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
	"github.com/google/uuid"
	_ "github.com/lib/pq"

	"github.com/mwhite7112/woodpantry-pantry/internal/api"
	"github.com/mwhite7112/woodpantry-pantry/internal/clients"
	"github.com/mwhite7112/woodpantry-pantry/internal/db"
	"github.com/mwhite7112/woodpantry-pantry/internal/events"
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
		return errors.New("DB_URL is required")
	}

	dictURL := os.Getenv("DICTIONARY_URL")
	if dictURL == "" {
		return errors.New("DICTIONARY_URL is required")
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		return errors.New("OPENAI_API_KEY is required")
	}

	extractModel := envOrDefault("EXTRACT_MODEL", "gpt-5-mini")
	rabbitMQURL := os.Getenv("RABBITMQ_URL")

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

	const httpClientTimeout = 30 * time.Second

	queries := db.New(sqlDB)
	httpClient := &http.Client{Timeout: httpClientTimeout}

	pantryPublisher, err := setupPantryUpdatedPublisher(rabbitMQURL)
	if err != nil {
		return err
	}
	defer pantryPublisher.Close()

	pantry := service.NewPantryService(queries, pantryPublisher)
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

type pantryPublisher interface {
	service.UpdatePublisher
	Close() error
}

func setupPantryUpdatedPublisher(rabbitMQURL string) (pantryPublisher, error) {
	if rabbitMQURL == "" {
		slog.Info("RABBITMQ_URL not set; pantry.updated publishing disabled")
		return nopCloserPublisher{}, nil
	}

	pub, err := events.NewPantryUpdatedPublisher(rabbitMQURL)
	if err != nil {
		slog.Warn("failed to initialize RabbitMQ publisher; pantry.updated publishing disabled", "error", err)
		return nopCloserPublisher{}, nil
	}

	slog.Info("RabbitMQ pantry.updated publisher enabled")
	return pub, nil
}

type nopCloserPublisher struct{}

func (nopCloserPublisher) PublishPantryUpdated(_ context.Context, _ []uuid.UUID) error {
	return nil
}

func (nopCloserPublisher) Close() error {
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
