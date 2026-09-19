// Package database opens the application's database connection.
package database

import (
	"fmt"
	"strings"

	"github.com/ncruces/go-sqlite3/gormlite"
	"gorm.io/gorm"
)

// Open opens and verifies a SQLite database connection without running migrations.
func Open(databaseURL string) (*gorm.DB, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("database URL is empty")
	}

	db, err := gorm.Open(gormlite.Open(databaseURL), &gorm.Config{
		DisableAutomaticPing: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database connection: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
