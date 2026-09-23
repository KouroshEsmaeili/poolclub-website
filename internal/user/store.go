// Package user provides persistence operations for user accounts.
package user

import (
	"context"
	"errors"
	"strings"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	// ErrEmailExists indicates that a normalized email is already registered.
	ErrEmailExists = errors.New("email already exists")
	// ErrNotFound indicates that a user account was not found.
	ErrNotFound = errors.New("user not found")
)

// Store persists and loads users.
type Store struct {
	db *gorm.DB
}

// NewStore creates a user store.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// NormalizeEmail applies the application email-normalization rule.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Create inserts a user unless the normalized email already exists.
func (s *Store) Create(
	ctx context.Context,
	email string,
	passwordHash string,
	firstName string,
	lastName string,
) (*model.User, error) {
	user := &model.User{
		Email:        NormalizeEmail(email),
		PasswordHash: passwordHash,
		FirstName:    &firstName,
		LastName:     &lastName,
	}

	result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "email"}},
		DoNothing: true,
	}).Create(user)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrEmailExists
	}

	return user, nil
}

// FindByEmail loads a user by normalized email.
func (s *Store) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var found model.User
	result := s.db.WithContext(ctx).Where("email = ?", NormalizeEmail(email)).First(&found)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &found, nil
}

// FindByID loads a user by primary key.
func (s *Store) FindByID(ctx context.Context, id int64) (*model.User, error) {
	var found model.User
	result := s.db.WithContext(ctx).First(&found, id)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &found, nil
}

// UpdatePasswordHash replaces a user's password hash.
func (s *Store) UpdatePasswordHash(ctx context.Context, id int64, passwordHash string) error {
	result := s.db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", id).
		Update("password_hash", passwordHash)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateProfile replaces the editable browser-profile fields atomically.
func (s *Store) UpdateProfile(ctx context.Context, id int64, email string, firstName string, lastName string, phone string, birthdate string, emergencyContact string) error {
	email = NormalizeEmail(email)
	result := s.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"email":             email,
			"first_name":        firstName,
			"last_name":         lastName,
			"phone":             phone,
			"birthdate":         birthdate,
			"emergency_contact": emergencyContact,
		})
	if result.Error != nil {
		if strings.Contains(strings.ToLower(result.Error.Error()), "unique") {
			return ErrEmailExists
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
