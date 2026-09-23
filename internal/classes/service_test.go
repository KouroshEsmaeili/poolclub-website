package classes

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

var classNow = time.Date(2030, time.January, 2, 10, 30, 0, 0, time.Local)

func TestEnrollPersistsFieldsAndChargesWalletExactly(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "success@example.com", 100000)

	result, err := service.Enroll(context.Background(), member.ID, "beginner")
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if result.NewBalance != 70000 {
		t.Fatalf("new balance = %d, want 70000", result.NewBalance)
	}
	if result.Enrollment.ID == "" || len(result.Enrollment.ID) != 36 || result.Enrollment.Status != StatusActive {
		t.Fatalf("enrollment identity/status = %+v", result.Enrollment)
	}

	stored := loadEnrollment(t, db, result.Enrollment.ID)
	if stored.UserID != member.ID || stored.ClassSlug != "beginner" || stored.ClassName != "مبتدی" || valueOrEmpty(stored.Coach) != "مربی یک" || valueOrEmpty(stored.Time) != "شنبه ۱۰" || stored.Price != 30000 || !stored.EnrolledAt.Equal(classNow) || stored.Status != StatusActive {
		t.Fatalf("stored enrollment = %+v", stored)
	}
	if got := loadClassBalance(t, db, member.ID); got != 70000 {
		t.Fatalf("stored balance = %d, want 70000", got)
	}
	transaction := onlyClassWalletTransaction(t, db, member.ID)
	if transaction.Amount != -30000 || transaction.Type != wallet.TransactionTypePurchase || valueOrEmpty(transaction.Description) != "ثبت‌نام در کلاس: مبتدی" {
		t.Fatalf("wallet transaction = %+v", transaction)
	}
}

func TestInsufficientFundsCreatesNoEnrollmentOrWalletHistory(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "poor@example.com", 29999)

	_, err := service.Enroll(context.Background(), member.ID, "beginner")
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Enroll() error = %v, want ErrInsufficientFunds", err)
	}
	if got := loadClassBalance(t, db, member.ID); got != 29999 {
		t.Fatalf("balance = %d, want 29999", got)
	}
	assertClassEnrollmentCount(t, db, member.ID, 0)
	assertClassWalletTransactionCount(t, db, member.ID, 0)
}

func TestUnknownInvalidAndUnavailableClassesDoNotCharge(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "invalid@example.com", 100000)

	if _, err := service.Enroll(context.Background(), member.ID, "missing"); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("missing class error = %v, want ErrClassNotFound", err)
	}
	if _, err := service.Enroll(context.Background(), member.ID, "invalid-price"); !errors.Is(err, ErrInvalidPrice) {
		t.Fatalf("invalid price error = %v, want ErrInvalidPrice", err)
	}
	unavailable := NewService(db, wallet.NewService(db), LoadCatalog(filepath.Join(t.TempDir(), "missing.json")))
	if _, err := unavailable.Enroll(context.Background(), member.ID, "beginner"); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("unavailable catalog error = %v, want ErrCatalogUnavailable", err)
	}
	assertClassEnrollmentCount(t, db, member.ID, 0)
	assertClassWalletTransactionCount(t, db, member.ID, 0)
}

func TestDuplicateEnrollmentIsAllowedAndChargedEachTime(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "duplicate@example.com", 100000)

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := service.Enroll(context.Background(), member.ID, "beginner"); err != nil {
			t.Fatalf("Enroll() attempt %d error = %v", attempt, err)
		}
	}
	if got := loadClassBalance(t, db, member.ID); got != 40000 {
		t.Fatalf("balance = %d, want 40000", got)
	}
	assertClassEnrollmentCount(t, db, member.ID, 2)
	assertClassWalletTransactionCount(t, db, member.ID, 2)
}

func TestConfiguredCapacityIsDisplayOnlyAndNotEnforced(t *testing.T) {
	db, service := classTestEnvironment(t)
	first := createClassUser(t, db, "capacity-one@example.com", 30000)
	second := createClassUser(t, db, "capacity-two@example.com", 30000)

	for _, member := range []model.User{first, second} {
		if _, err := service.Enroll(context.Background(), member.ID, "beginner"); err != nil {
			t.Fatalf("Enroll() for user %d error = %v", member.ID, err)
		}
	}
	var count int64
	if err := db.Model(&model.ClassEnrollment{}).Where("class_slug = ?", "beginner").Count(&count).Error; err != nil {
		t.Fatalf("count class enrollments: %v", err)
	}
	if count != 2 {
		t.Fatalf("capacity-1 class enrollment count = %d, want 2 for established duplicate-enrollment behavior", count)
	}
}

func TestCallerOwnedTransactionCanRollBackEnrollmentAndCharge(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "caller-rollback@example.com", 100000)
	ctx := context.Background()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if _, err := service.EnrollWithDB(ctx, tx, member.ID, "beginner"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("EnrollWithDB() error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if got := loadClassBalance(t, db, member.ID); got != 100000 {
		t.Fatalf("balance after rollback = %d, want 100000", got)
	}
	assertClassEnrollmentCount(t, db, member.ID, 0)
	assertClassWalletTransactionCount(t, db, member.ID, 0)
}

func TestEnrollmentInsertFailureRollsBackWalletCharge(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "insert-failure@example.com", 100000)
	if err := db.Exec(`
		CREATE TRIGGER reject_class_enrollment
		BEFORE INSERT ON class_enrollments
		BEGIN
			SELECT RAISE(ABORT, 'forced enrollment failure');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := service.Enroll(context.Background(), member.ID, "beginner"); err == nil {
		t.Fatal("Enroll() error = nil, want forced insertion failure")
	}
	if got := loadClassBalance(t, db, member.ID); got != 100000 {
		t.Fatalf("balance after rollback = %d, want 100000", got)
	}
	assertClassEnrollmentCount(t, db, member.ID, 0)
	assertClassWalletTransactionCount(t, db, member.ID, 0)
}

func TestEnrollmentsAreDeterministicAndIncludeAllStatuses(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "history@example.com", 0)
	insertClassEnrollment(t, db, model.ClassEnrollment{
		ID: "b", UserID: member.ID, ClassSlug: "second", ClassName: "second",
		Price: 2, EnrolledAt: classNow.Add(time.Hour), Status: "cancelled",
	})
	insertClassEnrollment(t, db, model.ClassEnrollment{
		ID: "a", UserID: member.ID, ClassSlug: "first", ClassName: "first",
		Price: 1, EnrolledAt: classNow, Status: StatusActive,
	})
	insertClassEnrollment(t, db, model.ClassEnrollment{
		ID: "c", UserID: member.ID, ClassSlug: "same-time", ClassName: "same-time",
		Price: 3, EnrolledAt: classNow.Add(time.Hour), Status: StatusActive,
	})

	history, err := service.Enrollments(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("Enrollments() error = %v", err)
	}
	if len(history) != 3 || history[0].ID != "a" || history[1].ID != "b" || history[2].ID != "c" {
		t.Fatalf("history order = %+v, want a, b, c", history)
	}
}

func TestConcurrentDuplicateEnrollmentsBothSucceed(t *testing.T) {
	db, service := classTestEnvironment(t)
	member := createClassUser(t, db, "concurrent@example.com", 60000)
	errorsFromEnrollments := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Enroll(context.Background(), member.ID, "beginner")
			errorsFromEnrollments <- err
		}()
	}
	group.Wait()
	close(errorsFromEnrollments)
	for err := range errorsFromEnrollments {
		if err != nil {
			t.Fatalf("concurrent Enroll() error = %v", err)
		}
	}
	if got := loadClassBalance(t, db, member.ID); got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
	assertClassEnrollmentCount(t, db, member.ID, 2)
	assertClassWalletTransactionCount(t, db, member.ID, 2)
}

func classTestEnvironment(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "classes.db"))
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
	if err := db.AutoMigrate(&model.User{}, &model.WalletTransaction{}, &model.ClassEnrollment{}); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}
	coach := "مربی یک"
	classTime := "شنبه ۱۰"
	service := NewService(db, wallet.NewService(db), NewCatalog([]Definition{
		{Slug: "beginner", Name: "مبتدی", Coach: &coach, Time: &classTime, PriceAmount: 30000, Capacity: 1},
		{Slug: "invalid-price", Name: "نامعتبر", PriceAmount: 0},
	}))
	service.now = func() time.Time { return classNow }
	return db, service
}

func createClassUser(t *testing.T, db *gorm.DB, email string, balance int64) model.User {
	t.Helper()
	empty := ""
	member := model.User{Email: email, PasswordHash: "not-used", FirstName: &empty, LastName: &empty, WalletBalance: balance}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return member
}

func loadEnrollment(t *testing.T, db *gorm.DB, enrollmentID string) model.ClassEnrollment {
	t.Helper()
	var enrollment model.ClassEnrollment
	if err := db.First(&enrollment, "id = ?", enrollmentID).Error; err != nil {
		t.Fatalf("load enrollment %q: %v", enrollmentID, err)
	}
	return enrollment
}

func insertClassEnrollment(t *testing.T, db *gorm.DB, enrollment model.ClassEnrollment) {
	t.Helper()
	if err := db.Create(&enrollment).Error; err != nil {
		t.Fatalf("insert enrollment %q: %v", enrollment.ID, err)
	}
}

func loadClassBalance(t *testing.T, db *gorm.DB, userID int64) int64 {
	t.Helper()
	var balance int64
	if err := db.Model(&model.User{}).Select("wallet_balance").Where("id = ?", userID).Scan(&balance).Error; err != nil {
		t.Fatalf("load balance: %v", err)
	}
	return balance
}

func onlyClassWalletTransaction(t *testing.T, db *gorm.DB, userID int64) model.WalletTransaction {
	t.Helper()
	return onlyClassWalletTransactionOfType(t, db, userID, "")
}

func onlyClassWalletTransactionOfType(t *testing.T, db *gorm.DB, userID int64, transactionType string) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	query := db.Where("user_id = ?", userID)
	if transactionType != "" {
		query = query.Where("type = ?", transactionType)
	}
	if err := query.Find(&transactions).Error; err != nil {
		t.Fatalf("load wallet transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("wallet transaction count = %d, want 1", len(transactions))
	}
	return transactions[0]
}

func assertClassEnrollmentCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.ClassEnrollment{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count class enrollments: %v", err)
	}
	if count != want {
		t.Fatalf("class enrollment count = %d, want %d", count, want)
	}
}

func assertClassWalletTransactionCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if count != want {
		t.Fatalf("wallet transaction count = %d, want %d", count, want)
	}
}

func assertClassWalletTransactionTypeCount(t *testing.T, db *gorm.DB, userID int64, transactionType string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ? AND type = ?", userID, transactionType).Count(&count).Error; err != nil {
		t.Fatalf("count %s wallet transactions: %v", transactionType, err)
	}
	if count != want {
		t.Fatalf("%s wallet transaction count = %d, want %d", transactionType, count, want)
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
