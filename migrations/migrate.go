// Package migrations applies the SQL migrations embedded in this repository.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed *.sql
var migrationFiles embed.FS

// Apply creates the migration tracking table and applies each pending SQL file
// in filename order. Each migration and its tracking row share a transaction.
func Apply(ctx context.Context, db *gorm.DB) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database is nil")
	}
	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version varchar(255) NOT NULL PRIMARY KEY,
			applied_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`).Error; err != nil {
		return nil, fmt.Errorf("create migration tracking table: %w", err)
	}

	entries, err := fs.Glob(migrationFiles, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(entries)

	applied := make([]string, 0, len(entries))
	for _, name := range entries {
		version := strings.TrimSuffix(name, ".sql")
		contents, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", version, err)
		}

		wasApplied := false
		err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var count int64
			if err := tx.Raw(
				"SELECT COUNT(*) FROM schema_migrations WHERE version = ?",
				version,
			).Scan(&count).Error; err != nil {
				return fmt.Errorf("check migration %s: %w", version, err)
			}
			if count != 0 {
				return nil
			}
			if err := tx.Exec(string(contents)).Error; err != nil {
				return fmt.Errorf("execute migration %s: %w", version, err)
			}
			if err := tx.Exec(
				"INSERT INTO schema_migrations (version) VALUES (?)",
				version,
			).Error; err != nil {
				return fmt.Errorf("record migration %s: %w", version, err)
			}
			wasApplied = true
			return nil
		})
		if err != nil {
			return nil, err
		}
		if wasApplied {
			applied = append(applied, version)
		}
	}
	return applied, nil
}
