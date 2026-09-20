package wallet_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

func TestDepositIncreasesBalanceAndCreatesTransaction(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	balance, err := service.Deposit(context.Background(), member.ID, 50, "test deposit")
	if err != nil {
		t.Fatalf("Deposit() error = %v", err)
	}
	if balance != 150 {
		t.Fatalf("Deposit() balance = %d, want 150", balance)
	}

	stored := loadWalletUser(t, db, member.ID)
	if stored.WalletBalance != 150 {
		t.Fatalf("stored balance = %d, want 150", stored.WalletBalance)
	}
	transaction := onlyWalletTransaction(t, db, member.ID)
	if transaction.Amount != 50 || transaction.Type != wallet.TransactionTypeDeposit {
		t.Fatalf("transaction = (%d, %q), want (50, deposit)", transaction.Amount, transaction.Type)
	}
	if transaction.Description == nil || *transaction.Description != "test deposit" {
		t.Fatalf("transaction description = %v, want test deposit", transaction.Description)
	}
}

func TestInvalidWalletAmountsAreRejected(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	for _, amount := range []int64{0, -1} {
		if _, err := service.Deposit(context.Background(), member.ID, amount, "invalid"); !errors.Is(err, wallet.ErrInvalidAmount) {
			t.Errorf("Deposit(%d) error = %v, want ErrInvalidAmount", amount, err)
		}
		if _, err := service.Charge(context.Background(), member.ID, amount, "invalid"); !errors.Is(err, wallet.ErrInvalidAmount) {
			t.Errorf("Charge(%d) error = %v, want ErrInvalidAmount", amount, err)
		}
	}

	if got := loadWalletUser(t, db, member.ID).WalletBalance; got != 100 {
		t.Fatalf("balance after invalid operations = %d, want 100", got)
	}
	assertTransactionCount(t, db, member.ID, 0)
}

func TestChargeCreatesNegativeTransactionAndRejectsInsufficientFunds(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	balance, err := service.Charge(context.Background(), member.ID, 40, "test purchase")
	if err != nil {
		t.Fatalf("Charge() error = %v", err)
	}
	if balance != 60 {
		t.Fatalf("Charge() balance = %d, want 60", balance)
	}
	transaction := onlyWalletTransaction(t, db, member.ID)
	if transaction.Amount != -40 || transaction.Type != wallet.TransactionTypePurchase {
		t.Fatalf("transaction = (%d, %q), want (-40, purchase)", transaction.Amount, transaction.Type)
	}

	if _, err := service.Charge(context.Background(), member.ID, 61, "too much"); !errors.Is(err, wallet.ErrInsufficientFunds) {
		t.Fatalf("excessive Charge() error = %v, want ErrInsufficientFunds", err)
	}
	if got := loadWalletUser(t, db, member.ID).WalletBalance; got != 60 {
		t.Fatalf("balance after rejected charge = %d, want 60", got)
	}
	assertTransactionCount(t, db, member.ID, 1)
}

func TestFailedTransactionRollsBackBalanceAndHistory(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	if err := db.Exec(`
		CREATE TRIGGER reject_wallet_history
		BEFORE INSERT ON wallet_transactions
		BEGIN
			SELECT RAISE(ABORT, 'forced history failure');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := service.Deposit(context.Background(), member.ID, 50, "must roll back"); err == nil {
		t.Fatal("Deposit() error = nil, want forced history failure")
	}
	if got := loadWalletUser(t, db, member.ID).WalletBalance; got != 100 {
		t.Fatalf("balance after failed transaction = %d, want 100", got)
	}
	assertTransactionCount(t, db, member.ID, 0)
}

func TestHistoryIsNewestFirst(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)
	ctx := context.Background()

	if _, err := service.Deposit(ctx, member.ID, 10, "first"); err != nil {
		t.Fatalf("first Deposit() error = %v", err)
	}
	if _, err := service.Deposit(ctx, member.ID, 20, "second"); err != nil {
		t.Fatalf("second Deposit() error = %v", err)
	}
	if _, err := service.Charge(ctx, member.ID, 5, "third"); err != nil {
		t.Fatalf("Charge() error = %v", err)
	}

	snapshot, err := service.Get(ctx, member.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	want := []string{"third", "second", "first"}
	if len(snapshot.Transactions) != len(want) {
		t.Fatalf("history length = %d, want %d", len(snapshot.Transactions), len(want))
	}
	for index, description := range want {
		if snapshot.Transactions[index].Description == nil || *snapshot.Transactions[index].Description != description {
			t.Errorf("history[%d] description = %v, want %q", index, snapshot.Transactions[index].Description, description)
		}
	}
}

func TestCallerOwnedTransactionCanRollBackChargeAndDependentWork(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)
	ctx := context.Background()

	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	if _, err := service.ChargeWithDB(ctx, tx, member.ID, 40, "dependent purchase"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("ChargeWithDB() error = %v", err)
	}
	if err := tx.Model(&model.User{}).Where("id = ?", member.ID).Update("first_name", "dependent work").Error; err != nil {
		_ = tx.Rollback()
		t.Fatalf("dependent update: %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}

	stored := loadWalletUser(t, db, member.ID)
	if stored.WalletBalance != 100 {
		t.Fatalf("balance after caller rollback = %d, want 100", stored.WalletBalance)
	}
	if stored.FirstName == nil || *stored.FirstName != "" {
		t.Fatalf("dependent field after rollback = %v, want empty", stored.FirstName)
	}
	assertTransactionCount(t, db, member.ID, 0)
}

func TestRefundCreatesRefundTransaction(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	balance, err := service.Refund(context.Background(), member.ID, 25, "membership refund")
	if err != nil {
		t.Fatalf("Refund() error = %v", err)
	}
	if balance != 125 {
		t.Fatalf("Refund() balance = %d, want 125", balance)
	}
	transaction := onlyWalletTransaction(t, db, member.ID)
	if transaction.Amount != 25 || transaction.Type != wallet.TransactionTypeRefund {
		t.Fatalf("transaction = (%d, %q), want (25, refund)", transaction.Amount, transaction.Type)
	}
}

func TestConcurrentChargesNeverOverdrawWallet(t *testing.T) {
	db := migratedWalletDatabase(t)
	member := createWalletUser(t, db, 100)
	service := wallet.NewService(db)

	const attempts = 20
	errorsFromCharges := make(chan error, attempts)
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Charge(context.Background(), member.ID, 10, "concurrent purchase")
			errorsFromCharges <- err
		}()
	}
	group.Wait()
	close(errorsFromCharges)

	var successful, insufficient int
	for err := range errorsFromCharges {
		switch {
		case err == nil:
			successful++
		case errors.Is(err, wallet.ErrInsufficientFunds):
			insufficient++
		default:
			t.Fatalf("concurrent Charge() error = %v", err)
		}
	}
	if successful != 10 || insufficient != 10 {
		t.Fatalf("concurrent results = %d successful, %d insufficient; want 10 and 10", successful, insufficient)
	}
	if got := loadWalletUser(t, db, member.ID).WalletBalance; got != 0 {
		t.Fatalf("balance after concurrent charges = %d, want 0", got)
	}
	assertTransactionCount(t, db, member.ID, 10)
}

func migratedWalletDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "wallet.db"))
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
	if err := db.AutoMigrate(&model.User{}, &model.WalletTransaction{}); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}
	return db
}

func createWalletUser(t *testing.T, db *gorm.DB, balance int64) model.User {
	t.Helper()
	empty := ""
	member := model.User{
		Email:         fmt.Sprintf("member-%d@example.com", balance),
		PasswordHash:  "not-used-by-wallet-tests",
		FirstName:     &empty,
		LastName:      &empty,
		WalletBalance: balance,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create wallet user: %v", err)
	}
	return member
}

func loadWalletUser(t *testing.T, db *gorm.DB, userID int64) model.User {
	t.Helper()
	var member model.User
	if err := db.First(&member, userID).Error; err != nil {
		t.Fatalf("load wallet user: %v", err)
	}
	return member
}

func onlyWalletTransaction(t *testing.T, db *gorm.DB, userID int64) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	if err := db.Where("user_id = ?", userID).Find(&transactions).Error; err != nil {
		t.Fatalf("load wallet transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("transaction count = %d, want 1", len(transactions))
	}
	return transactions[0]
}

func assertTransactionCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if count != want {
		t.Fatalf("transaction count = %d, want %d", count, want)
	}
}
