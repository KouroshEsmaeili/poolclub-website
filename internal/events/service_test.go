package events

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

var eventNow = time.Date(2030, time.February, 3, 11, 45, 0, 0, time.UTC)

func TestAuthenticatedRegistrationPersistsProfileAndChargesExactly(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "paid@example.com", "علی", "رضایی", 100000)

	result, err := service.Register(context.Background(), member.ID, "paid")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.NewBalance != 70000 || result.RegisteredCount != 1 {
		t.Fatalf("result balance/count = %d/%d, want 70000/1", result.NewBalance, result.RegisteredCount)
	}
	stored := loadEventRegistration(t, db, result.Registration.ID)
	if stored.UserID == nil || *stored.UserID != member.ID || stored.EventSlug != "paid" || stored.Title != "مسابقه" || valueOrEmpty(stored.Name) != "علی رضایی" || valueOrEmpty(stored.Email) != member.Email || stored.Price != 30000 || !stored.CreatedAt.Equal(eventNow) || stored.Status != StatusRegistered {
		t.Fatalf("stored registration = %+v", stored)
	}
	if got := loadEventBalance(t, db, member.ID); got != 70000 {
		t.Fatalf("balance = %d, want 70000", got)
	}
	transaction := onlyEventWalletTransaction(t, db, member.ID)
	if transaction.Amount != -30000 || transaction.Type != wallet.TransactionTypePurchase || valueOrEmpty(transaction.Description) != "ثبت‌نام رویداد: مسابقه" {
		t.Fatalf("wallet transaction = %+v", transaction)
	}
}

func TestAuthenticatedFreeEventCreatesNoWalletTransaction(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "free@example.com", "Free", "User", 1234)

	result, err := service.Register(context.Background(), member.ID, "free")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.NewBalance != 1234 || result.Registration.Price != 0 {
		t.Fatalf("free result = %+v", result)
	}
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestInsufficientFundsCreatesNoRegistrationOrHistory(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "poor@example.com", "Poor", "User", 29999)

	_, err := service.Register(context.Background(), member.ID, "paid")
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Register() error = %v, want ErrInsufficientFunds", err)
	}
	if got := loadEventBalance(t, db, member.ID); got != 29999 {
		t.Fatalf("balance = %d, want 29999", got)
	}
	assertEventRegistrationCount(t, db, "paid", 0)
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestAuthenticatedRegistrationValidatesPublishedOpenDuplicateAndCapacityRules(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "rules@example.com", "Rule", "Tester", 100000)

	if _, err := service.Register(context.Background(), member.ID, "missing"); !errors.Is(err, ErrEventNotFound) {
		t.Fatalf("missing event error = %v", err)
	}
	if _, err := service.Register(context.Background(), member.ID, "closed"); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("closed event error = %v", err)
	}
	if _, err := service.Register(context.Background(), member.ID, "paid"); err != nil {
		t.Fatalf("first paid registration error = %v", err)
	}
	if _, err := service.Register(context.Background(), member.ID, "paid"); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("duplicate error = %v, want ErrAlreadyRegistered", err)
	}
	assertEventRegistrationCount(t, db, "paid", 1)
	assertEventWalletTransactionCount(t, db, member.ID, 1)

	fullService := NewService(db, wallet.NewService(db), NewCatalog([]Definition{
		{Slug: "paid", Title: "مسابقه", State: "open", Price: 30000, Capacity: eventInt64(1)},
	}))
	if _, err := fullService.Register(context.Background(), member.ID, "paid"); !errors.Is(err, ErrCapacityReached) {
		t.Fatalf("full-before-duplicate error = %v, want ErrCapacityReached", err)
	}
}

func TestCancelledRegistrationDoesNotBlockAuthenticatedRegistration(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "cancelled@example.com", "Cancelled", "User", 0)
	insertEventRegistration(t, db, model.EventRegistration{
		UserID: &member.ID, EventSlug: "free", Title: "رایگان", Price: 0,
		CreatedAt: eventNow.Add(-time.Hour), Status: "cancelled",
	})

	if _, err := service.Register(context.Background(), member.ID, "free"); err != nil {
		t.Fatalf("Register() after cancelled row error = %v", err)
	}
	assertEventRegistrationCount(t, db, "free", 2)
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestCapacityCountsOnlyRegisteredRowsIncludingPublicRegistrations(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "capacity@example.com", "Capacity", "User", 30000)
	insertEventRegistration(t, db, model.EventRegistration{
		EventSlug: "limited", Title: "محدود", Name: eventString("Cancelled"), Email: eventString("cancelled@example.com"),
		Price: 0, CreatedAt: eventNow.Add(-time.Hour), Status: "cancelled",
	})
	if _, err := service.PublicRegister(context.Background(), nil, "limited", "Guest", "guest@example.com"); err != nil {
		t.Fatalf("PublicRegister() error = %v", err)
	}
	if _, err := service.Register(context.Background(), member.ID, "limited"); !errors.Is(err, ErrCapacityReached) {
		t.Fatalf("Register() error = %v, want ErrCapacityReached", err)
	}
	if got := loadEventBalance(t, db, member.ID); got != 30000 {
		t.Fatalf("balance = %d, want 30000", got)
	}
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestPublicRegistrationRequiresIdentityButAllowsDuplicatesAndIgnoresStateCapacityAndPrice(t *testing.T) {
	db, service := eventTestEnvironment(t)
	if _, err := service.PublicRegister(context.Background(), nil, "closed-full-paid", "", ""); !errors.Is(err, ErrIdentityRequired) {
		t.Fatalf("missing identity error = %v, want ErrIdentityRequired", err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		registration, err := service.PublicRegister(context.Background(), nil, "closed-full-paid", "Guest", "guest@example.com")
		if err != nil {
			t.Fatalf("PublicRegister() attempt %d error = %v", attempt, err)
		}
		if registration.UserID != nil || registration.Price != 0 || registration.Status != StatusRegistered {
			t.Fatalf("public registration = %+v", registration)
		}
	}
	assertEventRegistrationCount(t, db, "closed-full-paid", 2)
	var walletCount int64
	if err := db.Model(&model.WalletTransaction{}).Count(&walletCount).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if walletCount != 0 {
		t.Fatalf("wallet transaction count = %d, want 0", walletCount)
	}
}

func TestAuthenticatedPublicRegistrationDefaultsProfileWithoutCharging(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "public-member@example.com", "مریم", "احمدی", 50000)

	registration, err := service.PublicRegister(context.Background(), &member.ID, "paid", "", "")
	if err != nil {
		t.Fatalf("PublicRegister() error = %v", err)
	}
	if registration.UserID == nil || *registration.UserID != member.ID || valueOrEmpty(registration.Name) != "مریم احمدی" || valueOrEmpty(registration.Email) != member.Email || registration.Price != 0 {
		t.Fatalf("registration = %+v", registration)
	}
	if got := loadEventBalance(t, db, member.ID); got != 50000 {
		t.Fatalf("balance = %d, want 50000", got)
	}
	if _, err := service.Register(context.Background(), member.ID, "paid"); !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("wallet registration after user-linked public registration error = %v, want ErrAlreadyRegistered", err)
	}
	if got := loadEventBalance(t, db, member.ID); got != 50000 {
		t.Fatalf("balance after duplicate rejection = %d, want 50000", got)
	}
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestCallerOwnedTransactionCanRollBackRegistrationAndCharge(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "outer@example.com", "Outer", "Transaction", 30000)
	ctx := context.Background()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if _, err := service.RegisterWithDB(ctx, tx, member.ID, "paid"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("RegisterWithDB() error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if got := loadEventBalance(t, db, member.ID); got != 30000 {
		t.Fatalf("balance after rollback = %d, want 30000", got)
	}
	assertEventRegistrationCount(t, db, "paid", 0)
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestRegistrationInsertFailureRollsBackWalletCharge(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "failure@example.com", "Failure", "User", 30000)
	if err := db.Exec(`
		CREATE TRIGGER reject_event_registration
		BEFORE INSERT ON event_registrations
		BEGIN
			SELECT RAISE(ABORT, 'forced registration failure');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := service.Register(context.Background(), member.ID, "paid"); err == nil {
		t.Fatal("Register() error = nil, want forced insertion failure")
	}
	if got := loadEventBalance(t, db, member.ID); got != 30000 {
		t.Fatalf("balance after rollback = %d, want 30000", got)
	}
	assertEventRegistrationCount(t, db, "paid", 0)
	assertEventWalletTransactionCount(t, db, member.ID, 0)
}

func TestRegistrationsAreDeterministicAndIncludeEveryStatus(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "history@example.com", "History", "User", 0)
	insertEventRegistration(t, db, model.EventRegistration{UserID: &member.ID, EventSlug: "second", Title: "second", Price: 2, CreatedAt: eventNow.Add(time.Hour), Status: "cancelled"})
	insertEventRegistration(t, db, model.EventRegistration{UserID: &member.ID, EventSlug: "first", Title: "first", Price: 1, CreatedAt: eventNow, Status: StatusRegistered})
	insertEventRegistration(t, db, model.EventRegistration{UserID: &member.ID, EventSlug: "third", Title: "third", Price: 3, CreatedAt: eventNow.Add(time.Hour), Status: StatusRegistered})

	history, err := service.Registrations(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("Registrations() error = %v", err)
	}
	if len(history) != 3 || history[0].EventSlug != "first" || history[1].EventSlug != "second" || history[2].EventSlug != "third" {
		t.Fatalf("history = %+v", history)
	}
}

func TestConcurrentRegistrationsForFinalCapacityAllowOneCharge(t *testing.T) {
	db, _ := eventTestEnvironment(t)
	service := NewService(db, wallet.NewService(db), NewCatalog([]Definition{
		{Slug: "final", Title: "ظرفیت نهایی", State: "open", Price: 100, Capacity: eventInt64(1)},
	}))
	first := createEventUser(t, db, "first-final@example.com", "First", "User", 100)
	second := createEventUser(t, db, "second-final@example.com", "Second", "User", 100)

	errorsFromRegistrations := make(chan error, 2)
	var group sync.WaitGroup
	for _, userID := range []int64{first.ID, second.ID} {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Register(context.Background(), userID, "final")
			errorsFromRegistrations <- err
		}()
	}
	group.Wait()
	close(errorsFromRegistrations)

	successes, capacityFailures := 0, 0
	for err := range errorsFromRegistrations {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrCapacityReached):
			capacityFailures++
		default:
			t.Fatalf("concurrent Register() error = %v", err)
		}
	}
	if successes != 1 || capacityFailures != 1 {
		t.Fatalf("success/capacity outcomes = %d/%d, want 1/1", successes, capacityFailures)
	}
	assertEventRegistrationCount(t, db, "final", 1)
	var transactionCount int64
	if err := db.Model(&model.WalletTransaction{}).Where("type = ?", wallet.TransactionTypePurchase).Count(&transactionCount).Error; err != nil {
		t.Fatalf("count purchase transactions: %v", err)
	}
	if transactionCount != 1 {
		t.Fatalf("purchase transaction count = %d, want 1", transactionCount)
	}
}

func TestConcurrentDuplicateAuthenticatedRegistrationsAllowOneCharge(t *testing.T) {
	db, service := eventTestEnvironment(t)
	member := createEventUser(t, db, "concurrent-duplicate@example.com", "Duplicate", "User", 60000)

	errorsFromRegistrations := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Register(context.Background(), member.ID, "paid")
			errorsFromRegistrations <- err
		}()
	}
	group.Wait()
	close(errorsFromRegistrations)

	successes, duplicateFailures := 0, 0
	for err := range errorsFromRegistrations {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrAlreadyRegistered):
			duplicateFailures++
		default:
			t.Fatalf("concurrent Register() error = %v", err)
		}
	}
	if successes != 1 || duplicateFailures != 1 {
		t.Fatalf("success/duplicate outcomes = %d/%d, want 1/1", successes, duplicateFailures)
	}
	if got := loadEventBalance(t, db, member.ID); got != 30000 {
		t.Fatalf("balance = %d, want 30000", got)
	}
	assertEventRegistrationCount(t, db, "paid", 1)
	assertEventWalletTransactionCount(t, db, member.ID, 1)
}

func eventTestEnvironment(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "events.db"))
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
	if err := db.AutoMigrate(&model.User{}, &model.WalletTransaction{}, &model.EventRegistration{}); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}
	service := NewService(db, wallet.NewService(db), NewCatalog([]Definition{
		{Slug: "paid", Title: "مسابقه", State: "open", Price: 30000, Capacity: eventInt64(5)},
		{Slug: "free", Title: "رایگان", State: "open", Price: 0},
		{Slug: "closed", Title: "بسته", State: "closed", Price: 100},
		{Slug: "limited", Title: "محدود", State: "open", Price: 30000, Capacity: eventInt64(1)},
		{Slug: "closed-full-paid", Title: "عمومی", State: "closed", Price: 90000, Capacity: eventInt64(0)},
	}))
	service.now = func() time.Time { return eventNow }
	return db, service
}

func createEventUser(t *testing.T, db *gorm.DB, email string, firstName string, lastName string, balance int64) model.User {
	t.Helper()
	user := model.User{Email: email, PasswordHash: "not-used", FirstName: &firstName, LastName: &lastName, WalletBalance: balance}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return user
}

func loadEventRegistration(t *testing.T, db *gorm.DB, registrationID int64) model.EventRegistration {
	t.Helper()
	var registration model.EventRegistration
	if err := db.First(&registration, registrationID).Error; err != nil {
		t.Fatalf("load event registration %d: %v", registrationID, err)
	}
	return registration
}

func insertEventRegistration(t *testing.T, db *gorm.DB, registration model.EventRegistration) {
	t.Helper()
	if err := db.Create(&registration).Error; err != nil {
		t.Fatalf("insert event registration: %v", err)
	}
}

func loadEventBalance(t *testing.T, db *gorm.DB, userID int64) int64 {
	t.Helper()
	var balance int64
	if err := db.Model(&model.User{}).Select("wallet_balance").Where("id = ?", userID).Scan(&balance).Error; err != nil {
		t.Fatalf("load wallet balance: %v", err)
	}
	return balance
}

func onlyEventWalletTransaction(t *testing.T, db *gorm.DB, userID int64) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	if err := db.Where("user_id = ?", userID).Find(&transactions).Error; err != nil {
		t.Fatalf("load wallet transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("wallet transaction count = %d, want 1", len(transactions))
	}
	return transactions[0]
}

func assertEventRegistrationCount(t *testing.T, db *gorm.DB, slug string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.EventRegistration{}).Where("event_slug = ?", slug).Count(&count).Error; err != nil {
		t.Fatalf("count event registrations: %v", err)
	}
	if count != want {
		t.Fatalf("event registration count for %q = %d, want %d", slug, count, want)
	}
}

func assertEventWalletTransactionCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if count != want {
		t.Fatalf("wallet transaction count = %d, want %d", count, want)
	}
}

func eventInt64(value int64) *int64 {
	return &value
}

func eventString(value string) *string {
	return &value
}
