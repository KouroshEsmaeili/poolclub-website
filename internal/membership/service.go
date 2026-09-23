// Package membership provides membership state, purchases, history, and cancellation.
package membership

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

const (
	StatusActive    = "active"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
)

var (
	ErrPlanNotFound       = errors.New("membership plan not found")
	ErrInvalidPlan        = errors.New("invalid membership plan")
	ErrMembershipNotFound = errors.New("membership history item not found")
	ErrMembershipInactive = errors.New("membership history item is not active")
	ErrCancellationWindow = errors.New("membership can only be cancelled on purchase day")
	ErrUserNotFound       = wallet.ErrUserNotFound
	ErrInsufficientFunds  = wallet.ErrInsufficientFunds
)

// Current contains the persisted current-membership fields and their derived active state.
type Current struct {
	Slug          *string
	Name          *string
	ExpiresAt     *time.Time
	Active        bool
	WalletBalance int64
}

// PurchaseResult contains the newly created history row and post-charge state.
type PurchaseResult struct {
	Membership model.MembershipHistoryItem
	Current    Current
	NewBalance int64
}

// CancelResult contains the cancelled row and post-refund wallet balance.
type CancelResult struct {
	Membership model.MembershipHistoryItem
	NewBalance int64
}

// Service performs membership operations against the application database.
type Service struct {
	db       *gorm.DB
	wallet   *wallet.Service
	catalog  Catalog
	now      func() time.Time
	location *time.Location
}

// NewService creates a membership service.
func NewService(db *gorm.DB, walletService *wallet.Service, catalog Catalog) *Service {
	return &Service{
		db:       db,
		wallet:   walletService,
		catalog:  catalog,
		now:      time.Now,
		location: time.Local,
	}
}

// Plans returns membership plans in their configured order.
func (s *Service) Plans() ([]Plan, error) {
	return s.catalog.Plans()
}

// CurrentMembership retrieves current membership fields and derives whether they are active.
func (s *Service) CurrentMembership(ctx context.Context, userID int64) (Current, error) {
	var current Current
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		today := dateOnly(s.now(), s.location)
		if err := refreshExpiredWithDB(ctx, tx, userID, today); err != nil {
			return err
		}

		var user model.User
		if err := tx.WithContext(ctx).Select(
			"id", "wallet_balance", "membership_slug", "membership_name", "membership_expires_at",
		).First(&user, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("load membership owner: %w", err)
		}

		current = currentFromUser(user)
		if user.MembershipSlug == nil || user.MembershipExpiresAt == nil || dateBefore(*user.MembershipExpiresAt, today) {
			return nil
		}

		var latest model.MembershipHistoryItem
		err := tx.WithContext(ctx).
			Where("user_id = ? AND plan_slug = ?", userID, *user.MembershipSlug).
			Order("purchased_at DESC").
			Order("id DESC").
			First(&latest).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load current membership history: %w", err)
		}
		current.Active = latest.Status == StatusActive && !dateBefore(latest.ExpiresAt, today)
		return nil
	})
	return current, err
}

// History returns membership history newest first after marking old active rows expired.
func (s *Service) History(ctx context.Context, userID int64) ([]model.MembershipHistoryItem, error) {
	history := make([]model.MembershipHistoryItem, 0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		today := dateOnly(s.now(), s.location)
		if err := ensureUserExists(ctx, tx, userID); err != nil {
			return err
		}
		if err := refreshExpiredWithDB(ctx, tx, userID, today); err != nil {
			return err
		}
		if err := tx.WithContext(ctx).
			Where("user_id = ?", userID).
			Order("purchased_at DESC").
			Order("id DESC").
			Find(&history).Error; err != nil {
			return fmt.Errorf("load membership history: %w", err)
		}
		return nil
	})
	return history, err
}

// Purchase validates, charges, activates, and records a membership atomically.
func (s *Service) Purchase(ctx context.Context, userID int64, planSlug string) (PurchaseResult, error) {
	var result PurchaseResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.PurchaseWithDB(ctx, tx, userID, planSlug)
		return err
	})
	return result, err
}

// PurchaseWithDB performs a purchase using a caller-owned transaction.
func (s *Service) PurchaseWithDB(ctx context.Context, db *gorm.DB, userID int64, planSlug string) (PurchaseResult, error) {
	plan, found, err := s.catalog.find(planSlug)
	if err != nil {
		return PurchaseResult{}, err
	}
	if !found {
		return PurchaseResult{}, ErrPlanNotFound
	}
	if plan.Price <= 0 || plan.DurationDays <= 0 {
		return PurchaseResult{}, ErrInvalidPlan
	}

	now := s.now().In(s.location)
	today := dateOnly(now, s.location)
	var user model.User
	if err := db.WithContext(ctx).Select(
		"id", "wallet_balance", "membership_slug", "membership_name", "membership_expires_at",
	).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PurchaseResult{}, ErrUserNotFound
		}
		return PurchaseResult{}, fmt.Errorf("load membership owner: %w", err)
	}

	startDate := today
	if user.MembershipSlug != nil && *user.MembershipSlug == plan.Slug && user.MembershipExpiresAt != nil && !dateBefore(*user.MembershipExpiresAt, today) {
		startDate = normalizedDate(*user.MembershipExpiresAt)
	}
	expiresAt := startDate.AddDate(0, 0, int(plan.DurationDays))

	description := fmt.Sprintf("خرید اشتراک %s", plan.Name)
	newBalance, err := s.wallet.ChargeWithDB(ctx, db, userID, plan.Price, description)
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("charge membership: %w", err)
	}

	if err := refreshExpiredWithDB(ctx, db, userID, today); err != nil {
		return PurchaseResult{}, err
	}
	update := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"membership_slug":       plan.Slug,
		"membership_name":       plan.Name,
		"membership_expires_at": expiresAt,
	})
	if update.Error != nil {
		return PurchaseResult{}, fmt.Errorf("update current membership: %w", update.Error)
	}
	if update.RowsAffected == 0 {
		return PurchaseResult{}, ErrUserNotFound
	}

	historyID, err := newUUID()
	if err != nil {
		return PurchaseResult{}, fmt.Errorf("create membership history ID: %w", err)
	}
	history := model.MembershipHistoryItem{
		ID:          historyID,
		UserID:      userID,
		PlanSlug:    plan.Slug,
		PlanName:    plan.Name,
		PurchasedAt: now,
		ExpiresAt:   expiresAt,
		Amount:      plan.Price,
		Status:      StatusActive,
	}
	if err := db.WithContext(ctx).Create(&history).Error; err != nil {
		return PurchaseResult{}, fmt.Errorf("create membership history: %w", err)
	}

	slug, name := plan.Slug, plan.Name
	current := Current{
		Slug:          &slug,
		Name:          &name,
		ExpiresAt:     &expiresAt,
		Active:        true,
		WalletBalance: newBalance,
	}
	return PurchaseResult{Membership: history, Current: current, NewBalance: newBalance}, nil
}

// Cancel marks an eligible history row cancelled and refunds it atomically.
func (s *Service) Cancel(ctx context.Context, userID int64, historyID string) (CancelResult, error) {
	var result CancelResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.CancelWithDB(ctx, tx, userID, historyID)
		return err
	})
	return result, err
}

// CancelWithDB performs cancellation using a caller-owned transaction.
func (s *Service) CancelWithDB(ctx context.Context, db *gorm.DB, userID int64, historyID string) (CancelResult, error) {
	var user model.User
	if err := db.WithContext(ctx).Select(
		"id", "wallet_balance", "membership_slug", "membership_name", "membership_expires_at",
	).First(&user, userID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CancelResult{}, ErrUserNotFound
		}
		return CancelResult{}, fmt.Errorf("load membership owner: %w", err)
	}

	var item model.MembershipHistoryItem
	if err := db.WithContext(ctx).
		Where("id = ? AND user_id = ?", historyID, userID).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return CancelResult{}, ErrMembershipNotFound
		}
		return CancelResult{}, fmt.Errorf("load membership history item: %w", err)
	}
	if item.Status != StatusActive {
		return CancelResult{}, ErrMembershipInactive
	}

	today := dateOnly(s.now(), s.location)
	if item.PurchasedAt.In(s.location).Format("2006-01-02") != today.Format("2006-01-02") {
		return CancelResult{}, ErrCancellationWindow
	}

	updated := db.WithContext(ctx).Model(&model.MembershipHistoryItem{}).
		Where("id = ? AND user_id = ? AND status = ?", historyID, userID, StatusActive).
		UpdateColumn("status", StatusCancelled)
	if updated.Error != nil {
		return CancelResult{}, fmt.Errorf("cancel membership history: %w", updated.Error)
	}
	if updated.RowsAffected == 0 {
		return CancelResult{}, ErrMembershipInactive
	}

	if user.MembershipSlug != nil && *user.MembershipSlug == item.PlanSlug && user.MembershipExpiresAt != nil && sameDate(*user.MembershipExpiresAt, item.ExpiresAt) {
		if err := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
			"membership_slug":       nil,
			"membership_name":       nil,
			"membership_expires_at": nil,
		}).Error; err != nil {
			return CancelResult{}, fmt.Errorf("clear current membership: %w", err)
		}
	}

	newBalance := user.WalletBalance
	if item.Amount > 0 {
		description := fmt.Sprintf("استرداد اشتراک %s", item.PlanName)
		refundedBalance, err := s.wallet.RefundWithDB(ctx, db, userID, item.Amount, description)
		if err != nil {
			return CancelResult{}, fmt.Errorf("refund membership: %w", err)
		}
		newBalance = refundedBalance
	}
	item.Status = StatusCancelled
	return CancelResult{Membership: item, NewBalance: newBalance}, nil
}

func currentFromUser(user model.User) Current {
	return Current{
		Slug:          user.MembershipSlug,
		Name:          user.MembershipName,
		ExpiresAt:     user.MembershipExpiresAt,
		WalletBalance: user.WalletBalance,
	}
}

func ensureUserExists(ctx context.Context, db *gorm.DB, userID int64) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
		return fmt.Errorf("check membership owner: %w", err)
	}
	if count == 0 {
		return ErrUserNotFound
	}
	return nil
}

func refreshExpiredWithDB(ctx context.Context, db *gorm.DB, userID int64, today time.Time) error {
	if err := db.WithContext(ctx).Model(&model.MembershipHistoryItem{}).
		Where("user_id = ? AND status = ? AND expires_at < ?", userID, StatusActive, today.Format("2006-01-02")).
		UpdateColumn("status", StatusExpired).Error; err != nil {
		return fmt.Errorf("expire membership history: %w", err)
	}
	return nil
}

func dateOnly(value time.Time, location *time.Location) time.Time {
	year, month, day := value.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

func normalizedDate(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}

func dateBefore(value time.Time, other time.Time) bool {
	return value.Format("2006-01-02") < other.Format("2006-01-02")
}

func sameDate(first time.Time, second time.Time) bool {
	return first.Format("2006-01-02") == second.Format("2006-01-02")
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
