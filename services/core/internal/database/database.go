package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the sql.DB connection
type DB struct {
	*sql.DB
}

// Config holds database configuration
type Config struct {
	Path string
}

// New creates a new database connection
func New(cfg Config) (*DB, error) {
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set pragmas for performance and safety
	pragmas := []string{
		"PRAGMA journal_mode=WAL",      // Write-Ahead Logging for better concurrency
		"PRAGMA synchronous=NORMAL",    // Balance between safety and speed
		"PRAGMA cache_size=-2000",      // 2MB cache
		"PRAGMA temp_store=MEMORY",     // Store temp tables in memory
		"PRAGMA mmap_size=30000000000", // Memory-mapped I/O
		"PRAGMA foreign_keys=ON",       // Enable foreign key constraints
		"PRAGMA busy_timeout=5000",     // Wait up to 5s for locks
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	// Apply schema migrations (goose-tracked, embedded in the binary).
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}
