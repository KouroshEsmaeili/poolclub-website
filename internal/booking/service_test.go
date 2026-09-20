package booking

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

var fixedNow = time.Date(2030, time.January, 2, 8, 0, 0, 0, time.Local)

func TestCreatePersistsBookingAndChargesWalletAtomically(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "success@example.com", 100000)

	result, err := service.Create(context.Background(), member.ID, CreateInput{
		Date: "2030-01-03", Time: "10:00", Duration: 120, Type: TypeFreeSwim,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.NewBalance != 60000 {
		t.Fatalf("new balance = %d, want 60000", result.NewBalance)
	}
	if result.Booking.Status != StatusActive || result.Booking.Lane != nil {
		t.Fatalf("booking status/lane = %q/%v, want active/nil", result.Booking.Status, result.Booking.Lane)
	}

	stored := loadBooking(t, db, result.Booking.ID)
	if stored.Date != "2030-01-03" || stored.Time != "10:00" || stored.Duration != 120 || stored.Type != TypeFreeSwim {
		t.Fatalf("stored booking = %+v", stored)
	}
	if got := loadBalance(t, db, member.ID); got != 60000 {
		t.Fatalf("stored balance = %d, want 60000", got)
	}
	transaction := onlyPurchase(t, db, member.ID)
	if transaction.Amount != -DefaultFreeSwimPrice || transaction.Type != wallet.TransactionTypePurchase {
		t.Fatalf("wallet transaction = (%d, %q), want (-40000, purchase)", transaction.Amount, transaction.Type)
	}
	if transaction.Description == nil || *transaction.Description != "رزرو سانس (شنای آزاد)" {
		t.Fatalf("wallet description = %v", transaction.Description)
	}
}

func TestCreateNormalizesLaneTypeAndAssignsFirstLane(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "lane@example.com", 100000)

	result, err := service.Create(context.Background(), member.ID, futureInput(TypeLaneTrainingRequest, "10:00", 60))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Booking.Type != TypeLaneTraining || result.Booking.Lane == nil || *result.Booking.Lane != 1 {
		t.Fatalf("booking type/lane = %q/%v, want lane training/1", result.Booking.Type, result.Booking.Lane)
	}
	if result.NewBalance != 20000 {
		t.Fatalf("new balance = %d, want 20000", result.NewBalance)
	}
}

func TestUnknownTypeIsPreservedAndUsesFreeSwimFallbackPrice(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "unknown-type@example.com", 100000)

	result, err := service.Create(context.Background(), member.ID, futureInput("private swim", "10:00", 120))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if result.Booking.Type != "private swim" || result.Booking.Lane != nil {
		t.Fatalf("booking type/lane = %q/%v, want preserved unknown type and no lane", result.Booking.Type, result.Booking.Lane)
	}
	if result.NewBalance != 60000 {
		t.Fatalf("new balance = %d, want 60000 flat fallback price", result.NewBalance)
	}
}

func TestInsufficientFundsLeavesNoBookingOrHistory(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "poor@example.com", DefaultFreeSwimPrice-1)

	_, err := service.Create(context.Background(), member.ID, futureInput(TypeFreeSwim, "10:00", 60))
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("Create() error = %v, want ErrInsufficientFunds", err)
	}
	assertBookingCount(t, db, 0)
	assertWalletTransactionCount(t, db, member.ID, 0)
	if got := loadBalance(t, db, member.ID); got != DefaultFreeSwimPrice-1 {
		t.Fatalf("balance after rejection = %d, want %d", got, DefaultFreeSwimPrice-1)
	}
}

func TestBookingInsertFailureRollsBackWalletCharge(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "rollback@example.com", 100000)
	if err := db.Exec(`
		CREATE TRIGGER reject_booking_insert
		BEFORE INSERT ON bookings
		BEGIN
			SELECT RAISE(ABORT, 'forced booking failure');
		END
	`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	if _, err := service.Create(context.Background(), member.ID, futureInput(TypeFreeSwim, "10:00", 60)); err == nil {
		t.Fatal("Create() error = nil, want forced booking failure")
	}
	assertBookingCount(t, db, 0)
	assertWalletTransactionCount(t, db, member.ID, 0)
	if got := loadBalance(t, db, member.ID); got != 100000 {
		t.Fatalf("balance after failed insert = %d, want 100000", got)
	}
}

func TestCallerOwnedTransactionCanRollBackBookingAndCharge(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "caller-rollback@example.com", 100000)
	ctx := context.Background()
	tx := db.WithContext(ctx).Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	if _, err := service.CreateWithDB(ctx, tx, member.ID, futureInput(TypeFreeSwim, "10:00", 60)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("CreateWithDB() error = %v", err)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatalf("rollback: %v", err)
	}

	assertBookingCount(t, db, 0)
	assertWalletTransactionCount(t, db, member.ID, 0)
	if got := loadBalance(t, db, member.ID); got != 100000 {
		t.Fatalf("balance after caller rollback = %d, want 100000", got)
	}
}

func TestCreateValidationRejectsInvalidOrPastValuesBeforeCharge(t *testing.T) {
	tests := []struct {
		name  string
		input CreateInput
		want  error
	}{
		{name: "zero duration", input: futureInput(TypeFreeSwim, "10:00", 0), want: ErrInvalidDuration},
		{name: "negative duration", input: futureInput(TypeFreeSwim, "10:00", -30), want: ErrInvalidDuration},
		{name: "empty type", input: futureInput("", "10:00", 60), want: ErrInvalidType},
		{name: "invalid date", input: CreateInput{Date: "2030-02-30", Time: "10:00", Duration: 60, Type: TypeFreeSwim}, want: ErrInvalidDate},
		{name: "invalid time", input: CreateInput{Date: "2030-01-03", Time: "25:00", Duration: 60, Type: TypeFreeSwim}, want: ErrInvalidTime},
		{name: "past", input: CreateInput{Date: "2030-01-01", Time: "10:00", Duration: 60, Type: TypeFreeSwim}, want: ErrPastBooking},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, service := bookingTestEnvironment(t)
			member := createBookingUser(t, db, test.name+"@example.com", 100000)
			if _, err := service.Create(context.Background(), member.ID, test.input); !errors.Is(err, test.want) {
				t.Fatalf("Create() error = %v, want %v", err, test.want)
			}
			assertBookingCount(t, db, 0)
			assertWalletTransactionCount(t, db, member.ID, 0)
		})
	}
}

func TestOverlapUsesHalfOpenIntervals(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "overlap@example.com", 200000)
	insertBooking(t, db, member.ID, futureInput(TypeFreeSwim, "10:00", 60), nil, StatusActive)

	if _, err := service.Create(context.Background(), member.ID, futureInput(TypeFreeSwim, "10:30", 60)); !errors.Is(err, ErrOverlap) {
		t.Fatalf("overlapping Create() error = %v, want ErrOverlap", err)
	}
	if _, err := service.Create(context.Background(), member.ID, futureInput(TypeFreeSwim, "11:00", 60)); err != nil {
		t.Fatalf("adjacent Create() error = %v, want nil", err)
	}
	assertWalletTransactionCount(t, db, member.ID, 1)
	if got := loadBalance(t, db, member.ID); got != 160000 {
		t.Fatalf("balance = %d, want 160000", got)
	}
}

func TestPoolCapacityBelowAtAndAboveLimit(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	seedPoolBookings(t, db, PoolMaxCapacity-1, "10:00")
	atCapacityUser := createBookingUser(t, db, "capacity-40@example.com", 100000)
	if _, err := service.Create(context.Background(), atCapacityUser.ID, futureInput(TypeFreeSwim, "10:00", 60)); err != nil {
		t.Fatalf("Create() at 39 swimmers error = %v", err)
	}

	overflowUser := createBookingUser(t, db, "capacity-overflow@example.com", 100000)
	if _, err := service.Create(context.Background(), overflowUser.ID, futureInput(TypeFreeSwim, "10:00", 60)); !errors.Is(err, ErrPoolCapacity) {
		t.Fatalf("Create() at 40 swimmers error = %v, want ErrPoolCapacity", err)
	}
	if got := countActiveType(t, db, TypeFreeSwim); got != PoolMaxCapacity {
		t.Fatalf("active free-swim count = %d, want %d", got, PoolMaxCapacity)
	}
	if got := loadBalance(t, db, overflowUser.ID); got != 100000 {
		t.Fatalf("overflow user balance = %d, want 100000", got)
	}
}

func TestLaneAssignmentUsesFirstAvailableOverlappingLane(t *testing.T) {
	t.Run("first lane", func(t *testing.T) {
		db, service := bookingTestEnvironment(t)
		member := createBookingUser(t, db, "first-lane@example.com", 100000)
		result, err := service.Create(context.Background(), member.ID, futureInput(TypeLaneTraining, "10:00", 60))
		if err != nil || result.Booking.Lane == nil || *result.Booking.Lane != 1 {
			t.Fatalf("Create() = lane %v, error %v; want lane 1", result.Booking.Lane, err)
		}
	})

	t.Run("occupied lanes", func(t *testing.T) {
		db, service := bookingTestEnvironment(t)
		seedLane(t, db, 1, "10:00", 60)
		seedLane(t, db, 2, "10:30", 60)
		member := createBookingUser(t, db, "third-lane@example.com", 100000)
		result, err := service.Create(context.Background(), member.ID, futureInput(TypeLaneTraining, "10:30", 30))
		if err != nil || result.Booking.Lane == nil || *result.Booking.Lane != 3 {
			t.Fatalf("Create() = lane %v, error %v; want lane 3", result.Booking.Lane, err)
		}
	})

	t.Run("adjacent lane is available", func(t *testing.T) {
		db, service := bookingTestEnvironment(t)
		seedLane(t, db, 1, "10:00", 60)
		member := createBookingUser(t, db, "adjacent-lane@example.com", 100000)
		result, err := service.Create(context.Background(), member.ID, futureInput(TypeLaneTraining, "11:00", 60))
		if err != nil || result.Booking.Lane == nil || *result.Booking.Lane != 1 {
			t.Fatalf("Create() = lane %v, error %v; want lane 1", result.Booking.Lane, err)
		}
	})

	t.Run("all lanes occupied", func(t *testing.T) {
		db, service := bookingTestEnvironment(t)
		for _, lane := range AvailableLanes {
			seedLane(t, db, lane, "10:00", 60)
		}
		member := createBookingUser(t, db, "no-lane@example.com", 100000)
		if _, err := service.Create(context.Background(), member.ID, futureInput(TypeLaneTraining, "10:30", 30)); !errors.Is(err, ErrNoLane) {
			t.Fatalf("Create() error = %v, want ErrNoLane", err)
		}
	})
}

func TestConcurrentFinalCapacitySlotIsAllocatedOnce(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	seedPoolBookings(t, db, PoolMaxCapacity-1, "10:00")
	users := []model.User{
		createBookingUser(t, db, "capacity-race-1@example.com", 100000),
		createBookingUser(t, db, "capacity-race-2@example.com", 100000),
	}
	errorsFromCreates := concurrentCreates(service, users, futureInput(TypeFreeSwim, "10:00", 60))
	assertConcurrentResult(t, errorsFromCreates, ErrPoolCapacity)
	if got := countActiveType(t, db, TypeFreeSwim); got != PoolMaxCapacity {
		t.Fatalf("active free-swim count = %d, want %d", got, PoolMaxCapacity)
	}
}

func TestConcurrentFinalLaneIsAllocatedOnce(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	for _, lane := range AvailableLanes[:len(AvailableLanes)-1] {
		seedLane(t, db, lane, "10:00", 60)
	}
	users := []model.User{
		createBookingUser(t, db, "lane-race-1@example.com", 100000),
		createBookingUser(t, db, "lane-race-2@example.com", 100000),
	}
	errorsFromCreates := concurrentCreates(service, users, futureInput(TypeLaneTraining, "10:00", 60))
	assertConcurrentResult(t, errorsFromCreates, ErrNoLane)
	var laneSix int64
	if err := db.Model(&model.Booking{}).Where("status = ? AND lane = ?", StatusActive, int64(6)).Count(&laneSix).Error; err != nil {
		t.Fatalf("count lane six: %v", err)
	}
	if laneSix != 1 {
		t.Fatalf("lane-six booking count = %d, want 1", laneSix)
	}
}

func TestConcurrentOverlappingBookingsForOneUserAreRejected(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "overlap-race@example.com", 200000)
	users := []model.User{member, member}
	errorsFromCreates := concurrentCreates(service, users, futureInput("خصوصی", "10:00", 60))
	assertConcurrentResult(t, errorsFromCreates, ErrOverlap)
	assertWalletTransactionCount(t, db, member.ID, 1)
}

func TestOwnerCanCancelRepeatedlyWithoutRefundIncludingPastBooking(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "cancel@example.com", 25000)
	booking := insertBooking(t, db, member.ID, CreateInput{
		Date: "2030-01-01", Time: "10:00", Duration: 60, Type: TypeFreeSwim,
	}, nil, StatusExpired)

	if err := service.Cancel(context.Background(), member.ID, booking.ID); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := service.Cancel(context.Background(), member.ID, booking.ID); err != nil {
		t.Fatalf("repeated Cancel() error = %v", err)
	}
	if got := loadBooking(t, db, booking.ID).Status; got != StatusCancelled {
		t.Fatalf("cancelled status = %q, want cancelled", got)
	}
	if got := loadBalance(t, db, member.ID); got != 25000 {
		t.Fatalf("balance after cancellation = %d, want 25000", got)
	}
	assertWalletTransactionCount(t, db, member.ID, 0)
	if err := service.Cancel(context.Background(), member.ID, booking.ID+1000); !errors.Is(err, ErrBookingNotFound) {
		t.Fatalf("missing Cancel() error = %v, want ErrBookingNotFound", err)
	}
}

func TestRefreshStatusesExpiresAtStartTime(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	member := createBookingUser(t, db, "refresh@example.com", 0)
	ongoing := insertBooking(t, db, member.ID, CreateInput{
		Date: "2030-01-02", Time: "07:30", Duration: 120, Type: TypeFreeSwim,
	}, nil, StatusActive)
	future := insertBooking(t, db, member.ID, futureInput(TypeFreeSwim, "10:00", 60), nil, StatusActive)

	changed, err := service.RefreshStatuses(context.Background())
	if err != nil {
		t.Fatalf("RefreshStatuses() error = %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}
	if got := loadBooking(t, db, ongoing.ID).Status; got != StatusExpired {
		t.Fatalf("ongoing status = %q, want expired", got)
	}
	if got := loadBooking(t, db, future.ID).Status; got != StatusActive {
		t.Fatalf("future status = %q, want active", got)
	}
}

func bookingTestEnvironment(t *testing.T) (*gorm.DB, *Service) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "booking.db"))
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
	if err := db.AutoMigrate(&model.User{}, &model.WalletTransaction{}, &model.Booking{}); err != nil {
		t.Fatalf("auto-migrate temporary database: %v", err)
	}
	service := NewService(db, wallet.NewService(db), DefaultPrices())
	service.now = func() time.Time { return fixedNow }
	service.location = time.Local
	return db, service
}

func futureInput(bookingType string, clock string, duration int64) CreateInput {
	return CreateInput{Date: "2030-01-03", Time: clock, Duration: duration, Type: bookingType}
}

func createBookingUser(t *testing.T, db *gorm.DB, email string, balance int64) model.User {
	t.Helper()
	empty := ""
	member := model.User{Email: email, PasswordHash: "not-used", FirstName: &empty, LastName: &empty, WalletBalance: balance}
	if err := db.Create(&member).Error; err != nil {
		t.Fatalf("create user %q: %v", email, err)
	}
	return member
}

func insertBooking(t *testing.T, db *gorm.DB, userID int64, input CreateInput, lane *int64, status string) model.Booking {
	t.Helper()
	booking := model.Booking{
		UserID: userID, Date: input.Date, Time: input.Time, Duration: input.Duration,
		Type: normalizeType(input.Type), Lane: lane, Status: status,
	}
	if err := db.Create(&booking).Error; err != nil {
		t.Fatalf("insert booking: %v", err)
	}
	return booking
}

func seedPoolBookings(t *testing.T, db *gorm.DB, count int, clock string) {
	t.Helper()
	for index := range count {
		member := createBookingUser(t, db, fmt.Sprintf("pool-seed-%d@example.com", index), 0)
		insertBooking(t, db, member.ID, futureInput(TypeFreeSwim, clock, 60), nil, StatusActive)
	}
}

func seedLane(t *testing.T, db *gorm.DB, lane int64, clock string, duration int64) {
	t.Helper()
	member := createBookingUser(t, db, fmt.Sprintf("lane-seed-%d-%s@example.com", lane, stringsForEmail(clock)), 0)
	insertBooking(t, db, member.ID, futureInput(TypeLaneTraining, clock, duration), &lane, StatusActive)
}

func stringsForEmail(value string) string {
	result := ""
	for _, character := range value {
		if character >= '0' && character <= '9' {
			result += string(character)
		}
	}
	return result
}

func concurrentCreates(service *Service, users []model.User, input CreateInput) []error {
	errorsFromCreates := make(chan error, len(users))
	var group sync.WaitGroup
	for _, member := range users {
		member := member
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Create(context.Background(), member.ID, input)
			errorsFromCreates <- err
		}()
	}
	group.Wait()
	close(errorsFromCreates)
	results := make([]error, 0, len(users))
	for err := range errorsFromCreates {
		results = append(results, err)
	}
	return results
}

func assertConcurrentResult(t *testing.T, results []error, expectedFailure error) {
	t.Helper()
	var successes, failures int
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, expectedFailure):
			failures++
		default:
			t.Fatalf("concurrent Create() error = %v", err)
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent results = %d successes, %d expected failures; want 1 and 1", successes, failures)
	}
}

func loadBooking(t *testing.T, db *gorm.DB, bookingID int64) model.Booking {
	t.Helper()
	var booking model.Booking
	if err := db.First(&booking, bookingID).Error; err != nil {
		t.Fatalf("load booking: %v", err)
	}
	return booking
}

func loadBalance(t *testing.T, db *gorm.DB, userID int64) int64 {
	t.Helper()
	var member model.User
	if err := db.Select("wallet_balance").First(&member, userID).Error; err != nil {
		t.Fatalf("load balance: %v", err)
	}
	return member.WalletBalance
}

func onlyPurchase(t *testing.T, db *gorm.DB, userID int64) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	if err := db.Where("user_id = ? AND type = ?", userID, wallet.TransactionTypePurchase).Find(&transactions).Error; err != nil {
		t.Fatalf("load wallet transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("purchase transaction count = %d, want 1", len(transactions))
	}
	return transactions[0]
}

func assertBookingCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.Booking{}).Count(&count).Error; err != nil {
		t.Fatalf("count bookings: %v", err)
	}
	if count != want {
		t.Fatalf("booking count = %d, want %d", count, want)
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

func countActiveType(t *testing.T, db *gorm.DB, bookingType string) int {
	t.Helper()
	var count int64
	if err := db.Model(&model.Booking{}).Where("status = ? AND type = ?", StatusActive, bookingType).Count(&count).Error; err != nil {
		t.Fatalf("count active bookings: %v", err)
	}
	return int(count)
}
