package database

import (
	"path/filepath"
	"testing"
)

func TestOpen(t *testing.T) {
	databaseURL := filepath.Join(t.TempDir(), "test.db")

	db, err := Open(databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("DB() error = %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	var tableCount int64
	if err := db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table'").Scan(&tableCount).Error; err != nil {
		t.Fatalf("inspect database tables: %v", err)
	}
	if tableCount != 0 {
		t.Fatalf("table count = %d, want 0", tableCount)
	}
}

func TestOpenRejectsEmptyDatabaseURL(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open() error = nil, want an error")
	}
}
