package store

import (
	"database/sql"
	"fmt"

	"github.com/graycodeai/across/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

// Open opens (creating) the Across SQLite DB with required pragmas.
func Open(home string) (*sql.DB, error) {
	if err := config.EnsureHome(home); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", config.DBPath(home)+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
