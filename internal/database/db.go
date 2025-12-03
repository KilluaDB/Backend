package database

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func EnsureDatabaseExists() error {
	host := os.Getenv("DB_HOST")
	if host == "" {
		return fmt.Errorf("DB_HOST environment variable is required")
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		return fmt.Errorf("DB_PORT environment variable is required")
	}

	adminUser := os.Getenv("DB_ADMIN_USER")
	if adminUser == "" {
		return fmt.Errorf("DB_ADMIN_USER environment variable is required")
	}
	adminPassword := os.Getenv("DB_ADMIN_PASSWORD")
	if adminPassword == "" {
		return fmt.Errorf("DB_ADMIN_PASSWORD environment variable is required")
	}
	database := os.Getenv("DB_DATABASE")
	if database == "" {
		return fmt.Errorf("DB_DATABASE environment variable is required")
	}

	userInfo := url.UserPassword(adminUser, adminPassword)
	dsn := fmt.Sprintf(
		"postgres://%s@%s:%s/postgres?sslmode=disable",
		userInfo.String(),
		host,
		port,
	)

	log.Printf("Checking if database '%s' exists...", database)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	err = pool.QueryRow(ctx, query, database).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if !exists {
		log.Printf("Database '%s' does not exist. Creating it...", database)

		// Create database (note: CREATE DATABASE cannot be run in a transaction)
		// We need to use Exec with a connection that's not in a transaction
		// Properly quote the database name to handle special characters
		quotedDBName := pgx.Identifier{database}.Sanitize()
		createQuery := fmt.Sprintf("CREATE DATABASE %s", quotedDBName)
		_, err = pool.Exec(ctx, createQuery)
		if err != nil {
			return fmt.Errorf("failed to create database: %w", err)
		}
		log.Printf("Database '%s' created successfully", database)
	} else {
		log.Printf("Database '%s' already exists", database)
	}

	return nil
}

func Connect() (*pgxpool.Pool, error) {
	host := os.Getenv("DB_HOST")
	if host == "" {
		return nil, fmt.Errorf("DB_HOST environment variable is required")
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		return nil, fmt.Errorf("DB_PORT environment variable is required")
	}
	user := os.Getenv("DB_USERNAME")
	if user == "" {
		return nil, fmt.Errorf("DB_USERNAME environment variable is required")
	}
	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		return nil, fmt.Errorf("DB_PASSWORD environment variable is required")
	}
	database := os.Getenv("DB_DATABASE")
	if database == "" {
		return nil, fmt.Errorf("DB_DATABASE environment variable is required")
	}

	// Build connection string using postgres:// URL format
	// Use url.UserPassword to properly encode username and password
	userInfo := url.UserPassword(user, password)
	encodedDatabase := url.PathEscape(database)

	dsn := fmt.Sprintf(
		"postgres://%s@%s:%s/%s?sslmode=disable",
		userInfo.String(),
		host,
		port,
		encodedDatabase,
	)

	log.Printf("Connecting to database: postgres://%s:***@%s:%s/%s", user, host, port, database)

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string (check your .env file): %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 5 * time.Minute
	config.MaxConnIdleTime = 1 * time.Minute

	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test the connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	Pool = pool
	log.Println("Database connection pool established successfully")
	return pool, nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
		log.Println("Database connection pool closed")
	}
}
