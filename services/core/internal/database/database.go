package database

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the sql.DB connection
type DB struct {
	*sql.DB
}

// Config holds database configuration
type Config struct {
	Path       string
	SchemaPath string
}

// New creates a new database connection
func New(cfg Config) (*DB, error) {
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set pragmas for performance and safety
	pragmas := []string{
		"PRAGMA journal_mode=WAL",           // Write-Ahead Logging for better concurrency
		"PRAGMA synchronous=NORMAL",         // Balance between safety and speed
		"PRAGMA cache_size=-2000",           // 2MB cache
		"PRAGMA temp_store=MEMORY",          // Store temp tables in memory
		"PRAGMA mmap_size=30000000000",      // Memory-mapped I/O
		"PRAGMA foreign_keys=ON",            // Enable foreign key constraints
		"PRAGMA busy_timeout=5000",          // Wait up to 5s for locks
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	// Load and execute schema if provided
	if cfg.SchemaPath != "" {
		if err := executeSchema(db, cfg.SchemaPath); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to execute schema: %w", err)
		}
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// executeSchema reads and executes SQL schema file
func executeSchema(db *sql.DB, path string) error {
	schema, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read schema file: %w", err)
	}

	if _, err := db.Exec(string(schema)); err != nil {
		return fmt.Errorf("failed to execute schema: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
