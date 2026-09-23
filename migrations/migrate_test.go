package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/booking"
	"github.com/KouroshEsmaeili/poolclub-website/internal/classes"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/events"
	"github.com/KouroshEsmaeili/poolclub-website/internal/membership"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"github.com/KouroshEsmaeili/poolclub-website/migrations"
	"gorm.io/gorm"
)

func TestApplyCreatesSchemaAndIsIdempotent(t *testing.T) {
	db, sqlDB := emptyDatabase(t)

	applied, err := migrations.Apply(context.Background(), db)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if fmt.Sprint(applied) != "[001_initial]" {
		t.Fatalf("first applied migrations = %v, want [001_initial]", applied)
	}

	wantTables := []string{
		"bookings",
		"class_enrollments",
		"event_registrations",
		"membership_history",
		"schema_migrations",
		"users",
		"wallet_transactions",
	}
	if got := tableNames(t, sqlDB); fmt.Sprint(got) != fmt.Sprint(wantTables) {
		t.Fatalf("tables = %v, want %v", got, wantTables)
	}
	assertMigrationTracking(t, sqlDB)
	assertUniqueEmail(t, sqlDB)
	assertUserForeignKeys(t, sqlDB)
	assertRepresentativeColumns(t, sqlDB)

	applied, err = migrations.Apply(context.Background(), db)
	if err != nil {
		t.Fatalf("apply migrations a second time: %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("second applied migrations = %v, want none", applied)
	}
	assertMigrationTracking(t, sqlDB)
}

func TestMigratedSchemaSupportsDomainOperations(t *testing.T) {
	db, _ := emptyDatabase(t)
	ctx := context.Background()
	if _, err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	hash, err := auth.HashPassword("demo-password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	users := user.NewStore(db)
	created, err := users.Create(ctx, "demo@example.invalid", hash, "Demo", "Swimmer")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if ok, _ := auth.VerifyPassword(created.PasswordHash, "demo-password"); !ok {
		t.Fatal("stored password could not be authenticated")
	}
	if _, err := users.Create(ctx, "DEMO@example.invalid", hash, "Duplicate", "User"); !errors.Is(err, user.ErrEmailExists) {
		t.Fatalf("duplicate email error = %v, want %v", err, user.ErrEmailExists)
	}

	walletService := wallet.NewService(db)
	if balance, err := walletService.Deposit(ctx, created.ID, 1_000, "migration test deposit"); err != nil || balance != 1_000 {
		t.Fatalf("deposit result: balance=%d err=%v", balance, err)
	}

	bookingService := booking.NewService(db, walletService, booking.Prices{FreeSwim: 10, LaneTraining: 20})
	future := time.Now().AddDate(0, 0, 2)
	if _, err := bookingService.Create(ctx, created.ID, booking.CreateInput{
		Date: future.Format("2006-01-02"), Time: "12:00", Duration: 60, Type: booking.TypeFreeSwim,
	}); err != nil {
		t.Fatalf("create booking: %v", err)
	}

	membershipService := membership.NewService(db, walletService, membership.NewCatalog([]membership.Plan{{
		Slug: "demo-month", Name: "Demo Month", DurationDays: 30, Price: 30,
	}}))
	if _, err := membershipService.Purchase(ctx, created.ID, "demo-month"); err != nil {
		t.Fatalf("purchase membership: %v", err)
	}

	coach, classTime := "Demo Coach", "Saturday 10:00"
	classService := classes.NewService(db, walletService, classes.NewCatalog([]classes.Definition{{
		Slug: "demo-class", Name: "Demo Class", Coach: &coach, Time: &classTime, PriceAmount: 40, Capacity: 10,
	}}))
	if _, err := classService.Enroll(ctx, created.ID, "demo-class"); err != nil {
		t.Fatalf("enroll in class: %v", err)
	}

	capacity := int64(10)
	eventService := events.NewService(db, walletService, events.NewCatalog([]events.Definition{{
		Slug: "demo-event", Title: "Demo Event", State: "open", Price: 50, Capacity: &capacity,
	}}))
	if _, err := eventService.Register(ctx, created.ID, "demo-event"); err != nil {
		t.Fatalf("register for event: %v", err)
	}

	assertCount(t, db, &model.User{}, 1)
	assertCount(t, db, &model.Booking{}, 1)
	assertCount(t, db, &model.MembershipHistoryItem{}, 1)
	assertCount(t, db, &model.ClassEnrollment{}, 1)
	assertCount(t, db, &model.EventRegistration{}, 1)
	assertCount(t, db, &model.WalletTransaction{}, 5)
}

func emptyDatabase(t *testing.T) (*gorm.DB, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "migration.db"))
	if err != nil {
		t.Fatalf("open temporary database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db, sqlDB
}

func tableNames(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tables: %v", err)
	}
	sort.Strings(names)
	return names
}

func assertMigrationTracking(t *testing.T, db *sql.DB) {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = '001_initial'`).Scan(&count); err != nil {
		t.Fatalf("query migration tracking: %v", err)
	}
	if count != 1 {
		t.Fatalf("001_initial tracking rows = %d, want 1", count)
	}
}

func assertUniqueEmail(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_list('users')`)
	if err != nil {
		t.Fatalf("query users indexes: %v", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan users index: %v", err)
		}
		if name == "ix_users_email" && unique == 1 {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate users indexes: %v", err)
	}
	if !found {
		t.Fatal("unique index ix_users_email is missing")
	}
}

func assertUserForeignKeys(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"wallet_transactions", "membership_history", "class_enrollments", "bookings", "event_registrations"} {
		rows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%q)`, table))
		if err != nil {
			t.Fatalf("query %s foreign keys: %v", table, err)
		}
		found := false
		for rows.Next() {
			var id, sequence int
			var target, from, to, onUpdate, onDelete, match string
			if err := rows.Scan(&id, &sequence, &target, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				rows.Close()
				t.Fatalf("scan %s foreign key: %v", table, err)
			}
			if target == "users" && from == "user_id" && to == "id" && onDelete == "NO ACTION" {
				found = true
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatalf("iterate %s foreign keys: %v", table, err)
		}
		rows.Close()
		if !found {
			t.Errorf("%s user_id foreign key with NO ACTION is missing", table)
		}
	}
}

func assertRepresentativeColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	want := map[string]map[string]struct {
		typeName string
		notNull  bool
		primary  bool
	}{
		"users": {
			"id": {typeName: "INTEGER", primary: true}, "email": {typeName: "varchar(255)", notNull: true},
			"membership_expires_at": {typeName: "date"},
		},
		"bookings": {
			"id": {typeName: "INTEGER", primary: true}, "date": {typeName: "varchar(10)", notNull: true},
			"time": {typeName: "varchar(5)", notNull: true}, "lane": {typeName: "integer"},
		},
		"event_registrations": {
			"id": {typeName: "INTEGER", primary: true}, "user_id": {typeName: "integer"},
		},
	}
	for table, columns := range want {
		got := columnDefinitions(t, db, table)
		for name, expected := range columns {
			actual, ok := got[name]
			if !ok {
				t.Errorf("%s.%s is missing", table, name)
				continue
			}
			if !strings.EqualFold(actual.typeName, expected.typeName) || actual.notNull != expected.notNull || actual.primary != expected.primary {
				t.Errorf("%s.%s = %+v, want %+v", table, name, actual, expected)
			}
		}
	}
}

type columnDefinition struct {
	typeName string
	notNull  bool
	primary  bool
}

func columnDefinitions(t *testing.T, db *sql.DB, table string) map[string]columnDefinition {
	t.Helper()
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%q)`, table))
	if err != nil {
		t.Fatalf("query %s columns: %v", table, err)
	}
	defer rows.Close()
	columns := make(map[string]columnDefinition)
	for rows.Next() {
		var position, notNull, primary int
		var name, typeName string
		var defaultValue sql.NullString
		if err := rows.Scan(&position, &name, &typeName, &notNull, &defaultValue, &primary); err != nil {
			t.Fatalf("scan %s column: %v", table, err)
		}
		columns[name] = columnDefinition{typeName: typeName, notNull: notNull == 1, primary: primary == 1}
	}
	return columns
}

func assertCount(t *testing.T, db *gorm.DB, value any, want int64) {
	t.Helper()
	var got int64
	if err := db.Model(value).Count(&got).Error; err != nil {
		t.Fatalf("count %T: %v", value, err)
	}
	if got != want {
		t.Fatalf("count %T = %d, want %d", value, got, want)
	}
}
