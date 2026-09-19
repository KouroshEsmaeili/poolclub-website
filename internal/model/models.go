// Package model contains the database models shared by the Go backend.
package model

import "time"

// User maps to the users table.
type User struct {
	ID                  int64      `gorm:"column:id;primaryKey;autoIncrement"`
	Email               string     `gorm:"column:email;type:varchar(255);not null;uniqueIndex:ix_users_email"`
	PasswordHash        string     `gorm:"column:password_hash;type:varchar(255);not null"`
	FirstName           *string    `gorm:"column:first_name;type:varchar(100);default:''"`
	LastName            *string    `gorm:"column:last_name;type:varchar(100);default:''"`
	WalletBalance       int64      `gorm:"column:wallet_balance;not null;default:0"`
	MembershipSlug      *string    `gorm:"column:membership_slug;type:varchar(100)"`
	MembershipName      *string    `gorm:"column:membership_name;type:varchar(200)"`
	MembershipExpiresAt *time.Time `gorm:"column:membership_expires_at;type:date"`
	Phone               *string    `gorm:"column:phone;type:varchar(50);default:''"`
	Birthdate           *string    `gorm:"column:birthdate;type:varchar(20);default:''"`
	EmergencyContact    *string    `gorm:"column:emergency_contact;type:varchar(255);default:''"`
}

// TableName returns the existing Flask table name.
func (User) TableName() string {
	return "users"
}

// WalletTransaction maps to the wallet_transactions table.
type WalletTransaction struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID      int64     `gorm:"column:user_id;not null"`
	Amount      int64     `gorm:"column:amount;not null"`
	Type        string    `gorm:"column:type;type:varchar(20);not null"`
	Timestamp   time.Time `gorm:"column:timestamp;type:datetime;not null;autoCreateTime"`
	Description *string   `gorm:"column:description;type:varchar(255);default:''"`
	User        *User     `gorm:"foreignKey:UserID;references:ID"`
}

// TableName returns the existing Flask table name.
func (WalletTransaction) TableName() string {
	return "wallet_transactions"
}

// MembershipHistoryItem maps to the membership_history table.
type MembershipHistoryItem struct {
	ID          string    `gorm:"column:id;type:varchar(36);primaryKey;not null"`
	UserID      int64     `gorm:"column:user_id;not null"`
	PlanSlug    string    `gorm:"column:plan_slug;type:varchar(100);not null"`
	PlanName    string    `gorm:"column:plan_name;type:varchar(200);not null"`
	PurchasedAt time.Time `gorm:"column:purchased_at;type:datetime;not null"`
	ExpiresAt   time.Time `gorm:"column:expires_at;type:date;not null"`
	Amount      int64     `gorm:"column:amount;not null"`
	Status      string    `gorm:"column:status;type:varchar(20);not null;default:active"`
	User        *User     `gorm:"foreignKey:UserID;references:ID"`
}

// TableName returns the existing Flask table name.
func (MembershipHistoryItem) TableName() string {
	return "membership_history"
}

// ClassEnrollment maps to the class_enrollments table.
type ClassEnrollment struct {
	ID         string    `gorm:"column:id;type:varchar(36);primaryKey;not null"`
	UserID     int64     `gorm:"column:user_id;not null"`
	ClassSlug  string    `gorm:"column:class_slug;type:varchar(100);not null"`
	ClassName  string    `gorm:"column:class_name;type:varchar(200);not null"`
	Coach      *string   `gorm:"column:coach;type:varchar(200)"`
	Time       *string   `gorm:"column:time;type:varchar(100)"`
	Price      int64     `gorm:"column:price;not null"`
	EnrolledAt time.Time `gorm:"column:enrolled_at;type:datetime;not null"`
	Status     string    `gorm:"column:status;type:varchar(20);not null;default:active"`
	User       *User     `gorm:"foreignKey:UserID;references:ID"`
}

// TableName returns the existing Flask table name.
func (ClassEnrollment) TableName() string {
	return "class_enrollments"
}

// Booking maps to the bookings table.
type Booking struct {
	ID       int64  `gorm:"column:id;primaryKey;autoIncrement"`
	UserID   int64  `gorm:"column:user_id;not null"`
	Date     string `gorm:"column:date;type:varchar(10);not null"`
	Time     string `gorm:"column:time;type:varchar(5);not null"`
	Duration int64  `gorm:"column:duration;not null"`
	Type     string `gorm:"column:type;type:varchar(50);not null"`
	Lane     *int64 `gorm:"column:lane"`
	Status   string `gorm:"column:status;type:varchar(20);not null;default:active"`
	User     *User  `gorm:"foreignKey:UserID;references:ID"`
}

// TableName returns the existing Flask table name.
func (Booking) TableName() string {
	return "bookings"
}

// EventRegistration maps to the event_registrations table.
type EventRegistration struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    *int64    `gorm:"column:user_id"`
	EventSlug string    `gorm:"column:event_slug;type:varchar(100);not null"`
	Title     string    `gorm:"column:title;type:varchar(255);not null"`
	Name      *string   `gorm:"column:name;type:varchar(255)"`
	Email     *string   `gorm:"column:email;type:varchar(255)"`
	Price     int64     `gorm:"column:price;not null;default:0"`
	CreatedAt time.Time `gorm:"column:created_at;type:datetime;not null;autoCreateTime"`
	Status    string    `gorm:"column:status;type:varchar(20);not null;default:registered"`
	User      *User     `gorm:"foreignKey:UserID;references:ID"`
}

// TableName returns the existing Flask table name.
func (EventRegistration) TableName() string {
	return "event_registrations"
}
