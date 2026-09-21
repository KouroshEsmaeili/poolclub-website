// Package classes provides class catalog lookup, enrollment, and enrollment history.
package classes

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

const StatusActive = "active"

var (
	ErrClassNotFound     = errors.New("class not found")
	ErrInvalidPrice      = errors.New("invalid class price")
	ErrUserNotFound      = wallet.ErrUserNotFound
	ErrInsufficientFunds = wallet.ErrInsufficientFunds
)

// EnrollResult contains the persisted enrollment and post-charge balance.
type EnrollResult struct {
	Enrollment model.ClassEnrollment
	NewBalance int64
}

// Service performs class enrollment operations.
type Service struct {
	db      *gorm.DB
	wallet  *wallet.Service
	catalog Catalog
	now     func() time.Time
}

// NewService creates a class-enrollment service.
func NewService(db *gorm.DB, walletService *wallet.Service, catalog Catalog) *Service {
	return &Service{db: db, wallet: walletService, catalog: catalog, now: time.Now}
}

// Classes returns the class catalog in source-file order.
func (s *Service) Classes() ([]Definition, error) {
	return s.catalog.Classes()
}

// Enroll validates, charges, and persists an enrollment atomically.
func (s *Service) Enroll(ctx context.Context, userID int64, classSlug string) (EnrollResult, error) {
	var result EnrollResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.EnrollWithDB(ctx, tx, userID, classSlug)
		return err
	})
	return result, err
}

// EnrollWithDB performs enrollment using a caller-owned transaction.
func (s *Service) EnrollWithDB(ctx context.Context, db *gorm.DB, userID int64, classSlug string) (EnrollResult, error) {
	class, found, err := s.catalog.find(classSlug)
	if err != nil {
		return EnrollResult{}, err
	}
	if !found {
		return EnrollResult{}, ErrClassNotFound
	}
	if class.PriceAmount <= 0 {
		return EnrollResult{}, ErrInvalidPrice
	}

	// Flask permits duplicate enrollments and displays, but does not enforce,
	// configured capacity. Keeping both behaviors is intentional for parity.
	description := fmt.Sprintf("ثبت‌نام در کلاس: %s", class.Name)
	newBalance, err := s.wallet.ChargeWithDB(ctx, db, userID, class.PriceAmount, description)
	if err != nil {
		return EnrollResult{}, fmt.Errorf("charge class enrollment: %w", err)
	}

	enrollmentID, err := newUUID()
	if err != nil {
		return EnrollResult{}, fmt.Errorf("create class enrollment ID: %w", err)
	}
	enrollment := model.ClassEnrollment{
		ID:         enrollmentID,
		UserID:     userID,
		ClassSlug:  class.Slug,
		ClassName:  class.Name,
		Coach:      cloneString(class.Coach),
		Time:       cloneString(class.Time),
		Price:      class.PriceAmount,
		EnrolledAt: s.now(),
		Status:     StatusActive,
	}
	if err := db.WithContext(ctx).Create(&enrollment).Error; err != nil {
		return EnrollResult{}, fmt.Errorf("create class enrollment: %w", err)
	}
	return EnrollResult{Enrollment: enrollment, NewBalance: newBalance}, nil
}

// Enrollments returns the user's complete enrollment history in deterministic
// oldest-first order, matching the Flask page's effective insertion order.
func (s *Service) Enrollments(ctx context.Context, userID int64) ([]model.ClassEnrollment, error) {
	if err := ensureUserExists(ctx, s.db, userID); err != nil {
		return nil, err
	}
	enrollments := make([]model.ClassEnrollment, 0)
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("enrolled_at ASC").
		Order("id ASC").
		Find(&enrollments).Error; err != nil {
		return nil, fmt.Errorf("load class enrollments: %w", err)
	}
	return enrollments, nil
}

func ensureUserExists(ctx context.Context, db *gorm.DB, userID int64) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
		return fmt.Errorf("check class enrollment owner: %w", err)
	}
	if count == 0 {
		return ErrUserNotFound
	}
	return nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
