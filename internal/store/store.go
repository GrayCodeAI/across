package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	if err := execOpenPragma(db, `PRAGMA foreign_keys=ON`); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: enable foreign keys: %w", err)
	}
	if err := execOpenPragma(db, `PRAGMA busy_timeout=5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: set busy timeout: %w", err)
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}

func execOpenPragma(db *sql.DB, query string) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := db.Exec(query); err == nil {
			return nil
		} else if !isSQLiteLockError(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func isSQLiteLockError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "database schema is locked")
}
