package database

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// runMigrations applies pending schema migrations via goose. It also
// bootstraps version tracking for databases created by the old ad hoc
// executeSchema/ALTER-TABLE path (pre-goose deploys have no
// goose_db_version table).
func runMigrations(db *sql.DB) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}
	goose.SetBaseFS(embedMigrations)

	if err := bootstrapPreGooseDB(db); err != nil {
		return fmt.Errorf("failed to bootstrap pre-goose database: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// bootstrapPreGooseDB detects a database that already has tables from the
// old ad hoc schema path but no goose_db_version tracking table, and seeds
// that table so goose treats already-applied migrations as applied instead
// of re-running them. 00001 (CREATE TABLE IF NOT EXISTS) is harmless to
// re-run either way, but 00002 (ALTER TABLE ADD COLUMN) errors if the
// columns already exist — so it's only marked applied when they do.
func bootstrapPreGooseDB(db *sql.DB) error {
	hasPlayers, err := tableExists(db, "players")
	if err != nil {
		return err
	}
	if !hasPlayers {
		return nil // fresh DB, nothing to bootstrap
	}
	hasGooseTable, err := tableExists(db, "goose_db_version")
	if err != nil {
		return err
	}
	if hasGooseTable {
		return nil // already goose-tracked
	}

	if _, err := goose.EnsureDBVersion(db); err != nil { // creates goose_db_version, seeds version 0
		return err
	}

	seedVersions := []int64{1}
	hasDefconCols, err := columnExists(db, "player_history", "tackles")
	if err != nil {
		return err
	}
	if hasDefconCols {
		seedVersions = append(seedVersions, 2)
	}
	for _, v := range seedVersions {
		if _, err := db.Exec(`INSERT INTO goose_db_version (version_id, is_applied) VALUES (?, 1)`, v); err != nil {
			return fmt.Errorf("failed to seed goose version %d: %w", v, err)
		}
	}
	return nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	return n > 0, err
}

func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}
