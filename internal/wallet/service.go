// Package wallet provides atomic wallet operations and wallet history.
package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"gorm.io/gorm"
)

const (
	TransactionTypeDeposit  = "deposit"
	TransactionTypePurchase = "purchase"
	TransactionTypeRefund   = "refund"

	DefaultDepositDescription = "شارژ کیف پول"
	DefaultChargeDescription  = "خرید یا رزرو"
	ManualDepositDescription  = "شارژ دستی کیف پول"
)

var (
	ErrInvalidAmount     = errors.New("wallet amount must be positive")
	ErrInsufficientFunds = errors.New("insufficient wallet funds")
	ErrUserNotFound      = errors.New("wallet user not found")
)

// Snapshot is the current wallet balance and its transaction history.
type Snapshot struct {
	Balance      int64
	Transactions []model.WalletTransaction
}

// Service performs wallet operations against the application database.
type Service struct {
	db *gorm.DB
}

// NewService creates a wallet service.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// Get returns a wallet balance and transaction history, newest first.
func (s *Service) Get(ctx context.Context, userID int64) (Snapshot, error) {
	var snapshot Snapshot
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Select("id", "wallet_balance").First(&user, userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrUserNotFound
			}
			return fmt.Errorf("load wallet balance: %w", err)
		}

		snapshot.Balance = user.WalletBalance
		snapshot.Transactions = make([]model.WalletTransaction, 0)
		if err := tx.Where("user_id = ?", userID).
			Order("timestamp DESC").
			Order("id DESC").
			Find(&snapshot.Transactions).Error; err != nil {
			return fmt.Errorf("load wallet history: %w", err)
		}
		return nil
	})
	return snapshot, err
}

// Deposit credits a wallet and records a deposit in one transaction.
func (s *Service) Deposit(ctx context.Context, userID int64, amount int64, description string) (int64, error) {
	var balance int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		balance, err = s.DepositWithDB(ctx, tx, userID, amount, description)
		return err
	})
	return balance, err
}

// DepositWithDB credits a wallet using a caller-owned transaction.
func (s *Service) DepositWithDB(ctx context.Context, db *gorm.DB, userID int64, amount int64, description string) (int64, error) {
	if description == "" {
		description = DefaultDepositDescription
	}
	return creditWithDB(ctx, db, userID, amount, TransactionTypeDeposit, description)
}

// Refund credits a wallet and records a refund in one transaction.
func (s *Service) Refund(ctx context.Context, userID int64, amount int64, description string) (int64, error) {
	var balance int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		balance, err = s.RefundWithDB(ctx, tx, userID, amount, description)
		return err
	})
	return balance, err
}

// RefundWithDB credits a refund using a caller-owned transaction.
func (s *Service) RefundWithDB(ctx context.Context, db *gorm.DB, userID int64, amount int64, description string) (int64, error) {
	return creditWithDB(ctx, db, userID, amount, TransactionTypeRefund, description)
}

// Charge debits a wallet and records a purchase in one transaction.
func (s *Service) Charge(ctx context.Context, userID int64, amount int64, description string) (int64, error) {
	var balance int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		balance, err = s.ChargeWithDB(ctx, tx, userID, amount, description)
		return err
	})
	return balance, err
}

// ChargeWithDB debits a wallet using a caller-owned transaction.
func (s *Service) ChargeWithDB(ctx context.Context, db *gorm.DB, userID int64, amount int64, description string) (int64, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}
	if description == "" {
		description = DefaultChargeDescription
	}

	result := db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ? AND wallet_balance >= ?", userID, amount).
		UpdateColumn("wallet_balance", gorm.Expr("wallet_balance - ?", amount))
	if result.Error != nil {
		return 0, fmt.Errorf("debit wallet: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return 0, classifyRejectedCharge(ctx, db, userID)
	}

	if err := createTransaction(ctx, db, userID, -amount, TransactionTypePurchase, description); err != nil {
		return 0, err
	}
	return loadBalance(ctx, db, userID)
}

func creditWithDB(ctx context.Context, db *gorm.DB, userID int64, amount int64, transactionType string, description string) (int64, error) {
	if amount <= 0 {
		return 0, ErrInvalidAmount
	}

	result := db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", userID).
		UpdateColumn("wallet_balance", gorm.Expr("wallet_balance + ?", amount))
	if result.Error != nil {
		return 0, fmt.Errorf("credit wallet: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return 0, ErrUserNotFound
	}

	if err := createTransaction(ctx, db, userID, amount, transactionType, description); err != nil {
		return 0, err
	}
	return loadBalance(ctx, db, userID)
}

func classifyRejectedCharge(ctx context.Context, db *gorm.DB, userID int64) error {
	var count int64
	if err := db.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).Count(&count).Error; err != nil {
		return fmt.Errorf("check wallet owner: %w", err)
	}
	if count == 0 {
		return ErrUserNotFound
	}
	return ErrInsufficientFunds
}

func createTransaction(ctx context.Context, db *gorm.DB, userID int64, amount int64, transactionType string, description string) error {
	transaction := model.WalletTransaction{
		UserID:      userID,
		Amount:      amount,
		Type:        transactionType,
		Description: &description,
	}
	if err := db.WithContext(ctx).Create(&transaction).Error; err != nil {
		return fmt.Errorf("create wallet transaction: %w", err)
	}
	return nil
}

func loadBalance(ctx context.Context, db *gorm.DB, userID int64) (int64, error) {
	var balance int64
	if err := db.WithContext(ctx).
		Model(&model.User{}).
		Select("wallet_balance").
		Where("id = ?", userID).
		Scan(&balance).Error; err != nil {
		return 0, fmt.Errorf("load updated wallet balance: %w", err)
	}
	return balance, nil
}
