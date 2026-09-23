package model_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"gorm.io/gorm"
)

type expectedColumn struct {
	databaseType string
	nullable     bool
	primaryKey   bool
}

type columnInfo struct {
	databaseType string
	notNull      bool
	primaryKey   bool
	defaultValue sql.NullString
}

var expectedSchema = map[string]map[string]expectedColumn{
	"users": {
		"id":                    {databaseType: "integer", primaryKey: true},
		"email":                 {databaseType: "varchar(255)"},
		"password_hash":         {databaseType: "varchar(255)"},
		"first_name":            {databaseType: "varchar(100)", nullable: true},
		"last_name":             {databaseType: "varchar(100)", nullable: true},
		"wallet_balance":        {databaseType: "integer"},
		"membership_slug":       {databaseType: "varchar(100)", nullable: true},
		"membership_name":       {databaseType: "varchar(200)", nullable: true},
		"membership_expires_at": {databaseType: "date", nullable: true},
		"phone":                 {databaseType: "varchar(50)", nullable: true},
		"birthdate":             {databaseType: "varchar(20)", nullable: true},
		"emergency_contact":     {databaseType: "varchar(255)", nullable: true},
	},
	"wallet_transactions": {
		"id":          {databaseType: "integer", primaryKey: true},
		"user_id":     {databaseType: "integer"},
		"amount":      {databaseType: "integer"},
		"type":        {databaseType: "varchar(20)"},
		"timestamp":   {databaseType: "datetime"},
		"description": {databaseType: "varchar(255)", nullable: true},
	},
	"membership_history": {
		"id":           {databaseType: "varchar(36)", primaryKey: true},
		"user_id":      {databaseType: "integer"},
		"plan_slug":    {databaseType: "varchar(100)"},
		"plan_name":    {databaseType: "varchar(200)"},
		"purchased_at": {databaseType: "datetime"},
		"expires_at":   {databaseType: "date"},
		"amount":       {databaseType: "integer"},
		"status":       {databaseType: "varchar(20)"},
	},
	"class_enrollments": {
		"id":          {databaseType: "varchar(36)", primaryKey: true},
		"user_id":     {databaseType: "integer"},
		"class_slug":  {databaseType: "varchar(100)"},
		"class_name":  {databaseType: "varchar(200)"},
		"coach":       {databaseType: "varchar(200)", nullable: true},
		"time":        {databaseType: "varchar(100)", nullable: true},
		"price":       {databaseType: "integer"},
		"enrolled_at": {databaseType: "datetime"},
		"status":      {databaseType: "varchar(20)"},
	},
	"bookings": {
		"id":       {databaseType: "integer", primaryKey: true},
		"user_id":  {databaseType: "integer"},
		"date":     {databaseType: "varchar(10)"},
		"time":     {databaseType: "varchar(5)"},
		"duration": {databaseType: "integer"},
		"type":     {databaseType: "varchar(50)"},
		"lane":     {databaseType: "integer", nullable: true},
		"status":   {databaseType: "varchar(20)"},
	},
	"event_registrations": {
		"id":         {databaseType: "integer", primaryKey: true},
		"user_id":    {databaseType: "integer", nullable: true},
		"event_slug": {databaseType: "varchar(100)"},
		"title":      {databaseType: "varchar(255)"},
		"name":       {databaseType: "varchar(255)", nullable: true},
		"email":      {databaseType: "varchar(255)", nullable: true},
		"price":      {databaseType: "integer"},
		"created_at": {databaseType: "datetime"},
		"status":     {databaseType: "varchar(20)"},
	},
}

func TestAutoMigrateSchema(t *testing.T) {
	db, sqlDB := migratedDatabase(t)

	assertTables(t, sqlDB)
	for table, expectedColumns := range expectedSchema {
		assertColumns(t, sqlDB, table, expectedColumns)
	}
	assertEmailUniqueIndex(t, sqlDB)
	assertUserForeignKeys(t, sqlDB)
	assertDefaults(t, sqlDB)

	if db.Migrator().HasTable("wallet_transaction") {
		t.Fatal("unexpected singular wallet_transaction table exists")
	}
}

func migratedDatabase(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()

	databaseURL := filepath.Join(t.TempDir(), "schema.db")
	db, err := database.Open(databaseURL)
	if err != nil {
		t.Fatalf("open temporary database: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get temporary database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close temporary database: %v", err)
		}
	})

	if err := db.AutoMigrate(
		&model.User{},
		&model.WalletTransaction{},
		&model.MembershipHistoryItem{},
		&model.ClassEnrollment{},
		&model.Booking{},
		&model.EventRegistration{},
	); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}

	return db, sqlDB
}

func assertTables(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}

	sort.Strings(tables)
	want := []string{
		"bookings",
		"class_enrollments",
		"event_registrations",
		"membership_history",
		"users",
		"wallet_transactions",
	}
	if fmt.Sprint(tables) != fmt.Sprint(want) {
		t.Fatalf("tables = %v, want %v", tables, want)
	}
}

func assertColumns(t *testing.T, db *sql.DB, table string, expected map[string]expectedColumn) {
	t.Helper()

	columns := columnsFor(t, db, table)
	if len(columns) != len(expected) {
		t.Fatalf("%s column count = %d, want %d", table, len(columns), len(expected))
	}

	for name, want := range expected {
		got, ok := columns[name]
		if !ok {
			t.Errorf("%s.%s is missing", table, name)
			continue
		}
		if !strings.EqualFold(got.databaseType, want.databaseType) {
			t.Errorf("%s.%s type = %q, want %q", table, name, got.databaseType, want.databaseType)
		}
		if got.primaryKey != want.primaryKey {
			t.Errorf("%s.%s primary key = %t, want %t", table, name, got.primaryKey, want.primaryKey)
		}
		if !want.primaryKey && got.notNull == want.nullable {
			t.Errorf("%s.%s nullable = %t, want %t", table, name, !got.notNull, want.nullable)
		}
	}
}

func columnsFor(t *testing.T, db *sql.DB, table string) map[string]columnInfo {
	t.Helper()

	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		t.Fatalf("query %s columns: %v", table, err)
	}
	defer rows.Close()

	columns := make(map[string]columnInfo)
	for rows.Next() {
		var (
			position     int
			name         string
			databaseType string
			notNull      int
			defaultValue sql.NullString
			primaryKey   int
		)
		if err := rows.Scan(&position, &name, &databaseType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		columns[name] = columnInfo{
			databaseType: databaseType,
			notNull:      notNull == 1,
			primaryKey:   primaryKey == 1,
			defaultValue: defaultValue,
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}

	return columns
}

func assertEmailUniqueIndex(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.Query(`PRAGMA index_list("users")`)
	if err != nil {
		t.Fatalf("query users indexes: %v", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			position int
			name     string
			unique   int
			origin   string
			partial  int
		)
		if err := rows.Scan(&position, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan users index: %v", err)
		}
		if name == "ix_users_email" {
			found = true
			if unique != 1 {
				t.Errorf("ix_users_email unique = %d, want 1", unique)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate users indexes: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close users indexes: %v", err)
	}
	if !found {
		t.Fatal("unique index ix_users_email is missing")
	}

	indexRows, err := db.Query(`PRAGMA index_info("ix_users_email")`)
	if err != nil {
		t.Fatalf("query ix_users_email columns: %v", err)
	}
	defer indexRows.Close()

	var indexedColumns []string
	for indexRows.Next() {
		var (
			position int
			columnID int
			name     string
		)
		if err := indexRows.Scan(&position, &columnID, &name); err != nil {
			t.Fatalf("scan ix_users_email column: %v", err)
		}
		indexedColumns = append(indexedColumns, name)
	}
	if err := indexRows.Err(); err != nil {
		t.Fatalf("iterate ix_users_email columns: %v", err)
	}
	if len(indexedColumns) != 1 || indexedColumns[0] != "email" {
		t.Fatalf("ix_users_email columns = %v, want [email]", indexedColumns)
	}
}

func assertUserForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()

	for _, table := range []string{
		"wallet_transactions",
		"membership_history",
		"class_enrollments",
		"bookings",
		"event_registrations",
	} {
		rows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%q)`, table))
		if err != nil {
			t.Fatalf("query %s foreign keys: %v", table, err)
		}

		found := false
		for rows.Next() {
			var (
				id       int
				sequence int
				target   string
				from     string
				to       string
				onUpdate string
				onDelete string
				match    string
			)
			if err := rows.Scan(&id, &sequence, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				rows.Close()
				t.Fatalf("scan %s foreign key: %v", table, err)
			}
			if target == "users" && from == "user_id" && to == "id" {
				found = true
				if onDelete != "NO ACTION" {
					t.Errorf("%s.user_id ON DELETE = %q, want NO ACTION", table, onDelete)
				}
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate %s foreign keys: %v", table, err)
		}
		rows.Close()
		if !found {
			t.Errorf("%s.user_id foreign key to users.id is missing", table)
		}
	}
}

func assertDefaults(t *testing.T, db *sql.DB) {
	t.Helper()

	expected := map[string]map[string]string{
		"users": {
			"first_name":        "",
			"last_name":         "",
			"wallet_balance":    "0",
			"phone":             "",
			"birthdate":         "",
			"emergency_contact": "",
		},
		"wallet_transactions": {"description": ""},
		"membership_history":  {"status": "active"},
		"class_enrollments":   {"status": "active"},
		"bookings":            {"status": "active"},
		"event_registrations": {"price": "0", "status": "registered"},
	}

	for table, defaults := range expected {
		columns := columnsFor(t, db, table)
		for column, want := range defaults {
			got := columns[column].defaultValue
			if !got.Valid {
				t.Errorf("%s.%s default is missing", table, column)
				continue
			}
			if value := strings.Trim(got.String, "'\""); value != want {
				t.Errorf("%s.%s default = %q, want %q", table, column, value, want)
			}
		}
	}
}
