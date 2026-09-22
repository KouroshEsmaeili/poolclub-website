// Package events provides event catalog lookup and registration behavior.
package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

const StatusRegistered = "registered"

var (
	ErrEventNotFound      = errors.New("event not found")
	ErrRegistrationClosed = errors.New("event registration closed")
	ErrCapacityReached    = errors.New("event capacity reached")
	ErrAlreadyRegistered  = errors.New("user already registered for event")
	ErrIdentityRequired   = errors.New("public registration identity required")
	ErrUserNotFound       = wallet.ErrUserNotFound
	ErrInsufficientFunds  = wallet.ErrInsufficientFunds
)

// RegisterResult contains the persisted row and authenticated post-registration state.
type RegisterResult struct {
	Registration    model.EventRegistration
	NewBalance      int64
	RegisteredCount int64
}

// Service performs event registration operations.
type Service struct {
	db      *gorm.DB
	wallet  *wallet.Service
	catalog Catalog
	now     func() time.Time
}

// NewService creates an event-registration service.
func NewService(db *gorm.DB, walletService *wallet.Service, catalog Catalog) *Service {
	return &Service{db: db, wallet: walletService, catalog: catalog, now: time.Now}
}

// Events returns the published catalog in source-file order.
func (s *Service) Events() ([]Definition, error) {
	return s.catalog.Events()
}

// PublicRegister creates the Flask public/guest registration. Public
// registration intentionally does not enforce state, capacity, or duplicates
// and never charges a wallet.
func (s *Service) PublicRegister(ctx context.Context, userID *int64, eventSlug string, name string, email string) (model.EventRegistration, error) {
	var registration model.EventRegistration
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		event, found, err := s.catalog.find(eventSlug)
		if err != nil {
			return err
		}
		if !found {
			return ErrEventNotFound
		}

		if userID != nil && (name == "" || email == "") {
			user, err := loadUser(ctx, tx, *userID)
			if err != nil {
				return err
			}
			if name == "" {
				name = valueOrEmpty(user.FirstName) + " " + valueOrEmpty(user.LastName)
			}
			if email == "" {
				email = user.Email
			}
		}
		if name == "" || email == "" {
			return ErrIdentityRequired
		}

		nameCopy, emailCopy := name, email
		registration = model.EventRegistration{
			UserID:    cloneInt64(userID),
			EventSlug: event.Slug,
			Title:     event.Title,
			Name:      &nameCopy,
			Email:     &emailCopy,
			Price:     0,
			CreatedAt: s.now(),
			Status:    StatusRegistered,
		}
		if err := tx.WithContext(ctx).Create(&registration).Error; err != nil {
			return fmt.Errorf("create public event registration: %w", err)
		}
		return nil
	})
	return registration, err
}

// Register validates and creates an authenticated event registration atomically.
func (s *Service) Register(ctx context.Context, userID int64, eventSlug string) (RegisterResult, error) {
	var result RegisterResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.RegisterWithDB(ctx, tx, userID, eventSlug)
		return err
	})
	return result, err
}

// RegisterWithDB performs authenticated registration in a caller-owned transaction.
func (s *Service) RegisterWithDB(ctx context.Context, db *gorm.DB, userID int64, eventSlug string) (RegisterResult, error) {
	event, found, err := s.catalog.find(eventSlug)
	if err != nil {
		return RegisterResult{}, err
	}
	if !found {
		return RegisterResult{}, ErrEventNotFound
	}
	if event.State != "open" {
		return RegisterResult{}, ErrRegistrationClosed
	}

	registeredCount, err := countRegistered(ctx, db, event.Slug)
	if err != nil {
		return RegisterResult{}, err
	}
	if event.Capacity != nil && registeredCount >= *event.Capacity {
		return RegisterResult{}, ErrCapacityReached
	}

	var duplicateCount int64
	if err := db.WithContext(ctx).Model(&model.EventRegistration{}).
		Where("user_id = ? AND event_slug = ? AND status = ?", userID, event.Slug, StatusRegistered).
		Count(&duplicateCount).Error; err != nil {
		return RegisterResult{}, fmt.Errorf("check duplicate event registration: %w", err)
	}
	if duplicateCount > 0 {
		return RegisterResult{}, ErrAlreadyRegistered
	}

	user, err := loadUser(ctx, db, userID)
	if err != nil {
		return RegisterResult{}, err
	}
	title := event.Title
	if title == "" {
		title = "رویداد"
	}
	newBalance := user.WalletBalance
	if event.Price > 0 {
		description := fmt.Sprintf("ثبت‌نام رویداد: %s", title)
		newBalance, err = s.wallet.ChargeWithDB(ctx, db, userID, event.Price, description)
		if err != nil {
			return RegisterResult{}, fmt.Errorf("charge event registration: %w", err)
		}
	}

	name := valueOrEmpty(user.FirstName) + " " + valueOrEmpty(user.LastName)
	email := user.Email
	registrationUserID := userID
	registration := model.EventRegistration{
		UserID:    &registrationUserID,
		EventSlug: event.Slug,
		Title:     title,
		Name:      &name,
		Email:     &email,
		Price:     event.Price,
		CreatedAt: s.now(),
		Status:    StatusRegistered,
	}
	if err := db.WithContext(ctx).Create(&registration).Error; err != nil {
		return RegisterResult{}, fmt.Errorf("create authenticated event registration: %w", err)
	}

	registeredCount, err = countRegistered(ctx, db, event.Slug)
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{
		Registration:    registration,
		NewBalance:      newBalance,
		RegisteredCount: registeredCount,
	}, nil
}

// Registrations returns all of a user's event rows in deterministic oldest-first order.
func (s *Service) Registrations(ctx context.Context, userID int64) ([]model.EventRegistration, error) {
	if _, err := loadUser(ctx, s.db, userID); err != nil {
		return nil, err
	}
	registrations := make([]model.EventRegistration, 0)
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at ASC").
		Order("id ASC").
		Find(&registrations).Error; err != nil {
		return nil, fmt.Errorf("load event registrations: %w", err)
	}
	return registrations, nil
}

// RegisteredCount returns the active registration count for an event.
func (s *Service) RegisteredCount(ctx context.Context, eventSlug string) (int64, error) {
	return countRegistered(ctx, s.db, eventSlug)
}

// UserIsRegistered reports whether the user has an active registration for an event.
func (s *Service) UserIsRegistered(ctx context.Context, userID int64, eventSlug string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.EventRegistration{}).
		Where("user_id = ? AND event_slug = ? AND status = ?", userID, eventSlug, StatusRegistered).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check user event registration: %w", err)
	}
	return count > 0, nil
}

func countRegistered(ctx context.Context, db *gorm.DB, eventSlug string) (int64, error) {
	var count int64
	if err := db.WithContext(ctx).Model(&model.EventRegistration{}).
		Where("event_slug = ? AND status = ?", eventSlug, StatusRegistered).
		Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count event registrations: %w", err)
	}
	return count, nil
}

func loadUser(ctx context.Context, db *gorm.DB, userID int64) (model.User, error) {
	var user model.User
	if err := db.WithContext(ctx).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, ErrUserNotFound
		}
		return model.User{}, fmt.Errorf("load event registration user: %w", err)
	}
	return user, nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
