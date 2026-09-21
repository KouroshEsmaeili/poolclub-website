package membership

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

var membershipNow = time.Date(2030, time.January, 2, 10, 30, 0, 0, time.Local)

var membershipPlans = []Plan{
	{Slug: "standard", Name: "استاندارد", DurationDays: 30, Price: 30000},
	{Slug: "short", Name: "کوتاه", DurationDays: 10, Price: 10000},
	{Slug: "invalid", Name: "نامعتبر", DurationDays: 0, Price: 0},
}

func TestPurchaseWithoutMembershipChargesExactlyAndCreatesHistory(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "new@example.com", 100000)

	result, err := service.Purchase(context.Background(), member.ID, "standard")
	if err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}
	if result.NewBalance != 70000 {
		t.Fatalf("new balance = %d, want 70000", result.NewBalance)
	}
	if got := result.Membership.ExpiresAt.Format("2006-01-02"); got != "2030-02-01" {
		t.Fatalf("expiry = %s, want 2030-02-01", got)
	}
	if result.Membership.Status != StatusActive || result.Membership.Amount != 30000 {
		t.Fatalf("history item = %+v", result.Membership)
	}

	stored := loadMembershipUser(t, db, member.ID)
	assertCurrentFields(t, stored, "standard", "استاندارد", "2030-02-01")
	history := membershipHistory(t, db, member.ID)
	if len(history) != 1 || history[0].ID == "" || len(history[0].ID) != 36 {
		t.Fatalf("history = %+v, want one UUID row", history)
	}
	transaction := onlyTransactionOfType(t, db, member.ID, wallet.TransactionTypePurchase)
	if transaction.Amount != -30000 || valueOrEmpty(transaction.Description) != "خرید اشتراک استاندارد" {
		t.Fatalf("purchase transaction = %+v", transaction)
	}
}

func TestPurchaseExpiryRulesMatchFlask(t *testing.T) {
	t.Run("active same plan extends from current expiry", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "extend@example.com", 100000)
		setCurrentMembership(t, db, member.ID, "standard", "استاندارد", dateAt(2030, time.January, 10))

		result, err := service.Purchase(context.Background(), member.ID, "standard")
		if err != nil {
			t.Fatalf("Purchase() error = %v", err)
		}
		if got := result.Membership.ExpiresAt.Format("2006-01-02"); got != "2030-02-09" {
			t.Fatalf("expiry = %s, want 2030-02-09", got)
		}
	})

	t.Run("expired same plan starts from today", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "expired@example.com", 100000)
		setCurrentMembership(t, db, member.ID, "standard", "استاندارد", dateAt(2030, time.January, 1))
		old := createHistory(t, db, member.ID, "old-expired", "standard", membershipNow.AddDate(0, 0, -40), dateAt(2030, time.January, 1), 30000, StatusActive)

		result, err := service.Purchase(context.Background(), member.ID, "standard")
		if err != nil {
			t.Fatalf("Purchase() error = %v", err)
		}
		if got := result.Membership.ExpiresAt.Format("2006-01-02"); got != "2030-02-01" {
			t.Fatalf("expiry = %s, want 2030-02-01", got)
		}
		if got := loadHistory(t, db, old.ID).Status; got != StatusExpired {
			t.Fatalf("old history status = %q, want expired", got)
		}
	})

	t.Run("different active plan starts from today", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "switch@example.com", 100000)
		setCurrentMembership(t, db, member.ID, "standard", "استاندارد", dateAt(2030, time.March, 1))

		result, err := service.Purchase(context.Background(), member.ID, "short")
		if err != nil {
			t.Fatalf("Purchase() error = %v", err)
		}
		if got := result.Membership.ExpiresAt.Format("2006-01-02"); got != "2030-01-12" {
			t.Fatalf("expiry = %s, want 2030-01-12", got)
		}
	})
}

func TestInsufficientFundsLeavesMembershipAndHistoryUnchanged(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "poor@example.com", 29999)

	_, err := service.Purchase(context.Background(), member.ID, "standard")
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Purchase() error = %v, want ErrInsufficientFunds", err)
	}
	stored := loadMembershipUser(t, db, member.ID)
	if stored.WalletBalance != 29999 || stored.MembershipSlug != nil || stored.MembershipExpiresAt != nil {
		t.Fatalf("user after rejection = %+v", stored)
	}
	assertMembershipHistoryCount(t, db, member.ID, 0)
	assertWalletTransactionCount(t, db, member.ID, 0)
}

func TestPurchaseFailuresAfterChargeRollBackEverything(t *testing.T) {
	t.Run("membership update failure", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "update-failure@example.com", 100000)
		if err := db.Exec(`
			CREATE TRIGGER reject_membership_update
			BEFORE UPDATE OF membership_slug ON users
			BEGIN
				SELECT RAISE(ABORT, 'forced membership update failure');
			END
		`).Error; err != nil {
			t.Fatalf("create update trigger: %v", err)
		}

		if _, err := service.Purchase(context.Background(), member.ID, "standard"); err == nil {
			t.Fatal("Purchase() error = nil, want forced update failure")
		}
		assertPurchaseRollback(t, db, member.ID, 100000)
	})

	t.Run("membership history failure", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "history-failure@example.com", 100000)
		if err := db.Exec(`
			CREATE TRIGGER reject_membership_history
			BEFORE INSERT ON membership_history
			BEGIN
				SELECT RAISE(ABORT, 'forced membership history failure');
			END
		`).Error; err != nil {
			t.Fatalf("create history trigger: %v", err)
		}

		if _, err := service.Purchase(context.Background(), member.ID, "standard"); err == nil {
			t.Fatal("Purchase() error = nil, want forced history failure")
		}
		assertPurchaseRollback(t, db, member.ID, 100000)
	})
}

func TestCallerOwnedPurchaseTransactionCanRollBack(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "caller-rollback@example.com", 100000)
	ctx := context.Background()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if _, err := service.PurchaseWithDB(ctx, tx, member.ID, "standard"); err != nil {
		_ = tx.Rollback()
		t.Fatalf("PurchaseWithDB() error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}
	assertPurchaseRollback(t, db, member.ID, 100000)
}

func TestCallerOwnedCancellationTransactionCanRollBack(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "cancel-caller-rollback@example.com", 100)
	expires := dateAt(2030, time.February, 1)
	setCurrentMembership(t, db, member.ID, "standard", "استاندارد", expires)
	item := createHistory(t, db, member.ID, "cancel-caller-rollback", "standard", membershipNow, expires, 30, StatusActive)
	ctx := context.Background()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if _, err := service.CancelWithDB(ctx, tx, member.ID, item.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CancelWithDB() error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	stored := loadMembershipUser(t, db, member.ID)
	assertCurrentFields(t, stored, "standard", "استاندارد", "2030-02-01")
	if stored.WalletBalance != 100 || loadHistory(t, db, item.ID).Status != StatusActive {
		t.Fatalf("state changed after caller rollback: user=%+v history=%+v", stored, loadHistory(t, db, item.ID))
	}
	assertWalletTransactionCount(t, db, member.ID, 0)
}

func TestInvalidAndUnknownPlansDoNotCharge(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "invalid@example.com", 100000)

	if _, err := service.Purchase(context.Background(), member.ID, "missing"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("missing plan error = %v, want ErrPlanNotFound", err)
	}
	if _, err := service.Purchase(context.Background(), member.ID, "invalid"); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("invalid plan error = %v, want ErrInvalidPlan", err)
	}
	assertPurchaseRollback(t, db, member.ID, 100000)
}

func TestCurrentMembershipAndHistoryExpireAndOrderDeterministically(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "history@example.com", 12345)
	setCurrentMembership(t, db, member.ID, "standard", "استاندارد", dateAt(2030, time.January, 1))
	purchased := membershipNow.Add(-time.Hour)
	createHistory(t, db, member.ID, "a", "standard", purchased, dateAt(2030, time.January, 1), 1, StatusActive)
	createHistory(t, db, member.ID, "b", "standard", purchased, dateAt(2030, time.February, 1), 2, StatusCancelled)

	current, err := service.CurrentMembership(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("CurrentMembership() error = %v", err)
	}
	if current.Active || current.WalletBalance != 12345 {
		t.Fatalf("current = %+v, want inactive with balance 12345", current)
	}
	history, err := service.History(context.Background(), member.ID)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 2 || history[0].ID != "b" || history[1].ID != "a" {
		t.Fatalf("history order = %+v, want b then a", history)
	}
	if history[1].Status != StatusExpired {
		t.Fatalf("expired history status = %q, want expired", history[1].Status)
	}
}

func TestCancellationRefundsFullAmountAndClearsMatchingCurrentMembership(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "cancel@example.com", 100000)
	purchased, err := service.Purchase(context.Background(), member.ID, "standard")
	if err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}

	cancelled, err := service.Cancel(context.Background(), member.ID, purchased.Membership.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if cancelled.NewBalance != 100000 || cancelled.Membership.Status != StatusCancelled {
		t.Fatalf("Cancel() result = %+v", cancelled)
	}
	stored := loadMembershipUser(t, db, member.ID)
	if stored.MembershipSlug != nil || stored.MembershipName != nil || stored.MembershipExpiresAt != nil {
		t.Fatalf("current membership was not cleared: %+v", stored)
	}
	if got := loadHistory(t, db, purchased.Membership.ID).Status; got != StatusCancelled {
		t.Fatalf("history status = %q, want cancelled", got)
	}
	refund := onlyTransactionOfType(t, db, member.ID, wallet.TransactionTypeRefund)
	if refund.Amount != 30000 || valueOrEmpty(refund.Description) != "استرداد اشتراک استاندارد" {
		t.Fatalf("refund transaction = %+v", refund)
	}
}

func TestZeroAmountLegacyCancellationHasNoRefund(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "zero-refund@example.com", 500)
	expires := dateAt(2030, time.February, 1)
	setCurrentMembership(t, db, member.ID, "legacy", "قدیمی", expires)
	item := createHistory(t, db, member.ID, "zero", "legacy", membershipNow, expires, 0, StatusActive)

	result, err := service.Cancel(context.Background(), member.ID, item.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if result.NewBalance != 500 {
		t.Fatalf("new balance = %d, want 500", result.NewBalance)
	}
	assertWalletTransactionCount(t, db, member.ID, 0)
}

func TestCancellationEligibilityOwnershipAndRepeatedCancellation(t *testing.T) {
	t.Run("previous purchase day is rejected", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "past-cancel@example.com", 100)
		item := createHistory(t, db, member.ID, "past", "standard", membershipNow.AddDate(0, 0, -1), dateAt(2030, time.February, 1), 30, StatusActive)
		if _, err := service.Cancel(context.Background(), member.ID, item.ID); !errors.Is(err, ErrCancellationWindow) {
			t.Fatalf("Cancel() error = %v, want ErrCancellationWindow", err)
		}
		if loadHistory(t, db, item.ID).Status != StatusActive {
			t.Fatal("past history changed after rejected cancellation")
		}
		assertWalletTransactionCount(t, db, member.ID, 0)
	})

	t.Run("another user cannot cancel", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		owner := createMembershipUser(t, db, "owner@example.com", 0)
		other := createMembershipUser(t, db, "other@example.com", 0)
		item := createHistory(t, db, owner.ID, "owned", "standard", membershipNow, dateAt(2030, time.February, 1), 30, StatusActive)
		if _, err := service.Cancel(context.Background(), other.ID, item.ID); !errors.Is(err, ErrMembershipNotFound) {
			t.Fatalf("cross-user Cancel() error = %v, want ErrMembershipNotFound", err)
		}
		if loadHistory(t, db, item.ID).Status != StatusActive {
			t.Fatal("owned history changed after cross-user cancellation")
		}
	})

	t.Run("repeated cancellation refunds once", func(t *testing.T) {
		db, service := membershipTestEnvironment(t)
		member := createMembershipUser(t, db, "repeat@example.com", 100)
		item := createHistory(t, db, member.ID, "repeat", "standard", membershipNow, dateAt(2030, time.February, 1), 30, StatusActive)
		if _, err := service.Cancel(context.Background(), member.ID, item.ID); err != nil {
			t.Fatalf("first Cancel() error = %v", err)
		}
		if _, err := service.Cancel(context.Background(), member.ID, item.ID); !errors.Is(err, ErrMembershipInactive) {
			t.Fatalf("second Cancel() error = %v, want ErrMembershipInactive", err)
		}
		if got := loadMembershipUser(t, db, member.ID).WalletBalance; got != 130 {
			t.Fatalf("balance = %d, want 130", got)
		}
		assertWalletTransactionCount(t, db, member.ID, 1)
	})
}

func TestCancellationRefundFailureRollsBackState(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "refund-failure@example.com", 100000)
	purchased, err := service.Purchase(context.Background(), member.ID, "standard")
	if err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}
	if err := db.Exec(`
		CREATE TRIGGER reject_refund_history
		BEFORE INSERT ON wallet_transactions
		WHEN NEW.type = 'refund'
		BEGIN
			SELECT RAISE(ABORT, 'forced refund failure');
		END
	`).Error; err != nil {
		t.Fatalf("create refund trigger: %v", err)
	}

	if _, err := service.Cancel(context.Background(), member.ID, purchased.Membership.ID); err == nil {
		t.Fatal("Cancel() error = nil, want forced refund failure")
	}
	if got := loadHistory(t, db, purchased.Membership.ID).Status; got != StatusActive {
		t.Fatalf("history status after rollback = %q, want active", got)
	}
	stored := loadMembershipUser(t, db, member.ID)
	assertCurrentFields(t, stored, "standard", "استاندارد", "2030-02-01")
	if stored.WalletBalance != 70000 {
		t.Fatalf("balance after rollback = %d, want 70000", stored.WalletBalance)
	}
	assertWalletTransactionCount(t, db, member.ID, 1)
}

func TestConcurrentDuplicatePurchasesSerializeAndExtendTwice(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "concurrent-purchase@example.com", 100000)
	errorsFromPurchases := concurrentOperations(2, func() error {
		_, err := service.Purchase(context.Background(), member.ID, "standard")
		return err
	})
	for _, err := range errorsFromPurchases {
		if err != nil {
			t.Fatalf("concurrent Purchase() error = %v", err)
		}
	}
	stored := loadMembershipUser(t, db, member.ID)
	assertCurrentFields(t, stored, "standard", "استاندارد", "2030-03-03")
	if stored.WalletBalance != 40000 {
		t.Fatalf("balance = %d, want 40000", stored.WalletBalance)
	}
	assertMembershipHistoryCount(t, db, member.ID, 2)
	assertWalletTransactionCount(t, db, member.ID, 2)
}

func TestConcurrentDuplicateCancellationRefundsOnce(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "concurrent-cancel@example.com", 100)
	item := createHistory(t, db, member.ID, "cancel-race", "standard", membershipNow, dateAt(2030, time.February, 1), 30, StatusActive)
	errorsFromCancellations := concurrentOperations(2, func() error {
		_, err := service.Cancel(context.Background(), member.ID, item.ID)
		return err
	})
	var succeeded, inactive int
	for _, err := range errorsFromCancellations {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrMembershipInactive):
			inactive++
		default:
			t.Fatalf("concurrent Cancel() error = %v", err)
		}
	}
	if succeeded != 1 || inactive != 1 {
		t.Fatalf("results = %d success, %d inactive; want 1 and 1", succeeded, inactive)
	}
	if got := loadMembershipUser(t, db, member.ID).WalletBalance; got != 130 {
		t.Fatalf("balance = %d, want 130", got)
	}
	assertWalletTransactionCount(t, db, member.ID, 1)
}

func TestConcurrentPurchaseAndCancellationRemainSerializable(t *testing.T) {
	db, service := membershipTestEnvironment(t)
	member := createMembershipUser(t, db, "purchase-cancel-race@example.com", 100000)
	first, err := service.Purchase(context.Background(), member.ID, "standard")
	if err != nil {
		t.Fatalf("initial Purchase() error = %v", err)
	}

	var group sync.WaitGroup
	errorsFromOperations := make(chan error, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		_, err := service.Purchase(context.Background(), member.ID, "standard")
		errorsFromOperations <- err
	}()
	go func() {
		defer group.Done()
		_, err := service.Cancel(context.Background(), member.ID, first.Membership.ID)
		errorsFromOperations <- err
	}()
	group.Wait()
	close(errorsFromOperations)
	for err := range errorsFromOperations {
		if err != nil {
			t.Fatalf("concurrent purchase/cancel error = %v", err)
		}
	}

	stored := loadMembershipUser(t, db, member.ID)
	if stored.WalletBalance != 70000 || stored.MembershipSlug == nil || *stored.MembershipSlug != "standard" {
		t.Fatalf("final user = %+v, want one paid active standard membership", stored)
	}
	if loadHistory(t, db, first.Membership.ID).Status != StatusCancelled {
		t.Fatal("first history was not cancelled")
	}
	assertMembershipHistoryCount(t, db, member.ID, 2)
	assertWalletTransactionCount(t, db, member.ID, 3)
}

func membershipTestEnvironment(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "membership.db"))
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
	if err := db.AutoMigrate(&model.User{}, &model.WalletTransaction{}, &model.MembershipHistoryItem{}); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}
	service := NewService(db, wallet.NewService(db), NewCatalog(membershipPlans))
	service.now = func() time.Time { return membershipNow }
	service.location = time.Local
	return db, service
}

func createMembershipUser(t *testing.T, db *gorm.DB, email string, balance int64) model.User {
	t.Helper()
	empty := ""
	member := model.User{
		Email: email, PasswordHash: "not-used", FirstName: &empty, LastName: &empty, WalletBalance: balance,
	}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return member
}

func setCurrentMembership(t *testing.T, db *gorm.DB, userID int64, slug string, name string, expiry time.Time) {
	t.Helper()
	if err := db.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"membership_slug": slug, "membership_name": name, "membership_expires_at": expiry,
	}).Error; err != nil {
		t.Fatalf("set current membership: %v", err)
	}
}

func createHistory(t *testing.T, db *gorm.DB, userID int64, id string, slug string, purchased time.Time, expires time.Time, amount int64, status string) model.MembershipHistoryItem {
	t.Helper()
	item := model.MembershipHistoryItem{
		ID: id, UserID: userID, PlanSlug: slug, PlanName: slug, PurchasedAt: purchased,
		ExpiresAt: expires, Amount: amount, Status: status,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create history %q: %v", id, err)
	}
	return item
}

func loadMembershipUser(t *testing.T, db *gorm.DB, userID int64) model.User {
	t.Helper()
	var member model.User
	if err := db.First(&member, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return member
}

func loadHistory(t *testing.T, db *gorm.DB, historyID string) model.MembershipHistoryItem {
	t.Helper()
	var item model.MembershipHistoryItem
	if err := db.First(&item, "id = ?", historyID).Error; err != nil {
		t.Fatalf("load history %q: %v", historyID, err)
	}
	return item
}

func membershipHistory(t *testing.T, db *gorm.DB, userID int64) []model.MembershipHistoryItem {
	t.Helper()
	var history []model.MembershipHistoryItem
	if err := db.Where("user_id = ?", userID).Order("purchased_at ASC").Find(&history).Error; err != nil {
		t.Fatalf("load membership history: %v", err)
	}
	return history
}

func onlyTransactionOfType(t *testing.T, db *gorm.DB, userID int64, transactionType string) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	if err := db.Where("user_id = ? AND type = ?", userID, transactionType).Find(&transactions).Error; err != nil {
		t.Fatalf("load %s transactions: %v", transactionType, err)
	}
	if len(transactions) != 1 {
		t.Fatalf("%s transaction count = %d, want 1", transactionType, len(transactions))
	}
	return transactions[0]
}

func assertPurchaseRollback(t *testing.T, db *gorm.DB, userID int64, balance int64) {
	t.Helper()
	stored := loadMembershipUser(t, db, userID)
	if stored.WalletBalance != balance || stored.MembershipSlug != nil || stored.MembershipExpiresAt != nil {
		t.Fatalf("user after rollback = %+v", stored)
	}
	assertMembershipHistoryCount(t, db, userID, 0)
	assertWalletTransactionCount(t, db, userID, 0)
}

func assertCurrentFields(t *testing.T, member model.User, slug string, name string, expiry string) {
	t.Helper()
	if member.MembershipSlug == nil || *member.MembershipSlug != slug || member.MembershipName == nil || *member.MembershipName != name || member.MembershipExpiresAt == nil || member.MembershipExpiresAt.Format("2006-01-02") != expiry {
		t.Fatalf("current membership = slug %v name %v expiry %v; want %q/%q/%q", member.MembershipSlug, member.MembershipName, member.MembershipExpiresAt, slug, name, expiry)
	}
}

func assertMembershipHistoryCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.MembershipHistoryItem{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count membership history: %v", err)
	}
	if count != want {
		t.Fatalf("membership history count = %d, want %d", count, want)
	}
}

func assertWalletTransactionCount(t *testing.T, db *gorm.DB, userID int64, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if count != want {
		t.Fatalf("wallet transaction count = %d, want %d", count, want)
	}
}

func concurrentOperations(count int, operation func() error) []error {
	results := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			results <- operation()
		}()
	}
	group.Wait()
	close(results)
	errorsFromOperations := make([]error, 0, count)
	for err := range results {
		errorsFromOperations = append(errorsFromOperations, err)
	}
	return errorsFromOperations
}

func dateAt(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
