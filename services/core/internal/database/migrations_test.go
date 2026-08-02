package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func newTempDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func userTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version'`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, n)
	}
	return names
}

func gooseVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1`).Scan(&v); err != nil {
		t.Fatalf("query goose version: %v", err)
	}
	return v
}

func TestRunMigrations_FreshDB(t *testing.T) {
	db := newTempDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if tables := userTables(t, db); len(tables) != 11 {
		t.Fatalf("got %d tables, want 11: %v", len(tables), tables)
	}
	if v := gooseVersion(t, db); v != 2 {
		t.Fatalf("goose version = %d, want 2", v)
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	db := newTempDB(t)

	if err := runMigrations(db); err != nil {
		t.Fatalf("first runMigrations: %v", err)
	}
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
	if v := gooseVersion(t, db); v != 2 {
		t.Fatalf("goose version = %d, want 2", v)
	}
}

// oldSchemaPostDefcon simulates a DB created by the old ad hoc
// executeSchema + ALTER TABLE path, after the DEFCON columns landed.
const oldSchemaPostDefcon = `
CREATE TABLE players (id INTEGER PRIMARY KEY, web_name TEXT);
CREATE TABLE player_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	player_id INTEGER,
	event INTEGER,
	clearances_blocks_interceptions INTEGER DEFAULT 0,
	tackles INTEGER DEFAULT 0,
	recoveries INTEGER DEFAULT 0
);
`

// oldSchemaPreDefcon simulates a DB that predates even the DEFCON columns.
const oldSchemaPreDefcon = `
CREATE TABLE players (id INTEGER PRIMARY KEY, web_name TEXT);
CREATE TABLE player_history (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	player_id INTEGER,
	event INTEGER
);
`

func TestRunMigrations_PreExistingDB_PostDefcon(t *testing.T) {
	db := newTempDB(t)
	if _, err := db.Exec(oldSchemaPostDefcon); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if v := gooseVersion(t, db); v != 2 {
		t.Fatalf("goose version = %d, want 2", v)
	}
}

func TestRunMigrations_PreExistingDB_PreDefcon(t *testing.T) {
	db := newTempDB(t)
	if _, err := db.Exec(oldSchemaPreDefcon); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}

	if err := runMigrations(db); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	for _, col := range []string{"clearances_blocks_interceptions", "tackles", "recoveries"} {
		ok, err := columnExists(db, "player_history", col)
		if err != nil {
			t.Fatalf("columnExists(%s): %v", col, err)
		}
		if !ok {
			t.Fatalf("column %s missing after migration", col)
		}
	}

	// Re-running must not error (a repeated ALTER TABLE ADD COLUMN would).
	if err := runMigrations(db); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
}
