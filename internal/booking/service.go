// Package booking provides booking validation, allocation, and persistence.
package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

const (
	TypeFreeSwim            = "شنای آزاد"
	TypeLaneTraining        = "لاین تمرین"
	TypeLaneTrainingRequest = "رزرو لاین تمرین"

	StatusActive    = "active"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"

	PoolMaxCapacity = 40
)

var AvailableLanes = [...]int64{1, 2, 3, 4, 5, 6}

var (
	ErrInvalidDate       = errors.New("invalid booking date")
	ErrInvalidTime       = errors.New("invalid booking time")
	ErrInvalidDuration   = errors.New("invalid booking duration")
	ErrInvalidType       = errors.New("invalid booking type")
	ErrPastBooking       = errors.New("booking starts in the past")
	ErrOverlap           = errors.New("booking overlaps an active user booking")
	ErrPoolCapacity      = errors.New("pool capacity is full")
	ErrNoLane            = errors.New("no training lane is available")
	ErrBookingNotFound   = errors.New("booking not found")
	ErrInsufficientFunds = wallet.ErrInsufficientFunds
)

// CreateInput contains the persisted booking request fields.
type CreateInput struct {
	Date     string
	Time     string
	Duration int64
	Type     string
}

// CreateResult contains the new booking and post-charge wallet balance.
type CreateResult struct {
	Booking    model.Booking
	NewBalance int64
}

// Service performs booking operations.
type Service struct {
	db       *gorm.DB
	wallet   *wallet.Service
	prices   Prices
	now      func() time.Time
	location *time.Location
}

// NewService creates a booking service.
func NewService(db *gorm.DB, walletService *wallet.Service, prices Prices) *Service {
	return &Service{
		db:       db,
		wallet:   walletService,
		prices:   prices,
		now:      time.Now,
		location: time.Local,
	}
}

// Create validates, allocates, charges, and persists a booking atomically.
func (s *Service) Create(ctx context.Context, userID int64, input CreateInput) (CreateResult, error) {
	var created CreateResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		created, err = s.CreateWithDB(ctx, tx, userID, input)
		return err
	})
	return created, err
}

// CreateWithDB performs booking creation in a caller-owned transaction.
func (s *Service) CreateWithDB(ctx context.Context, db *gorm.DB, userID int64, input CreateInput) (CreateResult, error) {
	if input.Duration <= 0 {
		return CreateResult{}, ErrInvalidDuration
	}
	start, err := parseStart(input.Date, input.Time, s.location)
	if err != nil {
		return CreateResult{}, err
	}
	if start.Before(s.now()) {
		return CreateResult{}, ErrPastBooking
	}
	end := start.Add(time.Duration(input.Duration) * time.Minute)
	bookingType := normalizeType(input.Type)
	if bookingType == "" {
		return CreateResult{}, ErrInvalidType
	}

	var active []model.Booking
	if err := db.WithContext(ctx).Where("status = ?", StatusActive).Find(&active).Error; err != nil {
		return CreateResult{}, fmt.Errorf("load active bookings: %w", err)
	}

	for _, existing := range active {
		if existing.UserID != userID {
			continue
		}
		if bookingOverlaps(existing, start, end, s.location) {
			return CreateResult{}, ErrOverlap
		}
	}

	var lane *int64
	switch bookingType {
	case TypeFreeSwim:
		if countPoolSwimmers(active, start, end, s.location) >= PoolMaxCapacity {
			return CreateResult{}, ErrPoolCapacity
		}
	case TypeLaneTraining:
		assigned, ok := firstAvailableLane(active, start, end, s.location)
		if !ok {
			return CreateResult{}, ErrNoLane
		}
		lane = &assigned
	}

	price := s.priceFor(bookingType)
	description := fmt.Sprintf("رزرو سانس (%s)", bookingType)
	newBalance, err := s.wallet.ChargeWithDB(ctx, db, userID, price, description)
	if err != nil {
		return CreateResult{}, fmt.Errorf("charge booking: %w", err)
	}

	created := model.Booking{
		UserID:   userID,
		Date:     input.Date,
		Time:     input.Time,
		Duration: input.Duration,
		Type:     bookingType,
		Lane:     lane,
		Status:   StatusActive,
	}
	if err := db.WithContext(ctx).Create(&created).Error; err != nil {
		return CreateResult{}, fmt.Errorf("create booking: %w", err)
	}
	return CreateResult{Booking: created, NewBalance: newBalance}, nil
}

// Cancel marks a user's booking as cancelled without changing wallet state.
func (s *Service) Cancel(ctx context.Context, userID int64, bookingID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return s.CancelWithDB(ctx, tx, userID, bookingID)
	})
}

// CancelWithDB cancels a booking using a caller-owned transaction.
func (s *Service) CancelWithDB(ctx context.Context, db *gorm.DB, userID int64, bookingID int64) error {
	result := db.WithContext(ctx).
		Model(&model.Booking{}).
		Where("id = ? AND user_id = ?", bookingID, userID).
		UpdateColumn("status", StatusCancelled)
	if result.Error != nil {
		return fmt.Errorf("cancel booking: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrBookingNotFound
	}
	return nil
}

// RefreshStatuses marks active bookings expired when their start is in the past.
func (s *Service) RefreshStatuses(ctx context.Context) (int64, error) {
	var changed int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var active []model.Booking
		if err := tx.Where("status = ?", StatusActive).Find(&active).Error; err != nil {
			return fmt.Errorf("load active bookings: %w", err)
		}
		for _, existing := range active {
			start, err := parseStart(existing.Date, existing.Time, s.location)
			if err == nil && !start.Before(s.now()) {
				continue
			}
			if err := tx.Model(&model.Booking{}).
				Where("id = ?", existing.ID).
				UpdateColumn("status", StatusExpired).Error; err != nil {
				return fmt.Errorf("expire booking: %w", err)
			}
			changed++
		}
		return nil
	})
	return changed, err
}

func (s *Service) priceFor(bookingType string) int64 {
	if bookingType == TypeLaneTraining {
		return s.prices.LaneTraining
	}
	return s.prices.FreeSwim
}

func normalizeType(bookingType string) string {
	bookingType = strings.TrimSpace(bookingType)
	if bookingType == TypeLaneTrainingRequest {
		return TypeLaneTraining
	}
	return bookingType
}

func parseStart(date string, clock string, location *time.Location) (time.Time, error) {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return time.Time{}, ErrInvalidDate
	}
	if _, err := time.Parse("15:04", clock); err != nil {
		return time.Time{}, ErrInvalidTime
	}
	start, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, location)
	if err != nil {
		return time.Time{}, ErrInvalidTime
	}
	return start, nil
}

func intervalsOverlap(firstStart time.Time, firstEnd time.Time, secondStart time.Time, secondEnd time.Time) bool {
	return firstStart.Before(secondEnd) && secondStart.Before(firstEnd)
}

func bookingOverlaps(existing model.Booking, start time.Time, end time.Time, location *time.Location) bool {
	existingStart, err := parseStart(existing.Date, existing.Time, location)
	if err != nil {
		return false
	}
	existingEnd := existingStart.Add(time.Duration(existing.Duration) * time.Minute)
	return intervalsOverlap(start, end, existingStart, existingEnd)
}

func countPoolSwimmers(active []model.Booking, start time.Time, end time.Time, location *time.Location) int {
	count := 0
	for _, existing := range active {
		if existing.Type == TypeFreeSwim && bookingOverlaps(existing, start, end, location) {
			count++
		}
	}
	return count
}

func firstAvailableLane(active []model.Booking, start time.Time, end time.Time, location *time.Location) (int64, bool) {
	for _, lane := range AvailableLanes {
		available := true
		for _, existing := range active {
			if existing.Lane != nil && *existing.Lane == lane && bookingOverlaps(existing, start, end, location) {
				available = false
				break
			}
		}
		if available {
			return lane, true
		}
	}
	return 0, false
}
