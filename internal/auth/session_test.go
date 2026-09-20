package auth

import (
	"testing"
	"time"
)

func TestSessionExpirationAndDeletion(t *testing.T) {
	now := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	store := NewSessionStore(time.Hour)
	store.now = func() time.Time { return now }

	id, expiresAt, err := store.Create(42)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if want := now.Add(time.Hour); !expiresAt.Equal(want) {
		t.Fatalf("expiration = %v, want %v", expiresAt, want)
	}
	if userID, ok := store.UserID(id); !ok || userID != 42 {
		t.Fatalf("UserID() = (%d, %t), want (42, true)", userID, ok)
	}

	store.Delete(id)
	if _, ok := store.UserID(id); ok {
		t.Fatal("UserID() accepted deleted session")
	}

	id, _, err = store.Create(42)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now = now.Add(2 * time.Hour)
	if _, ok := store.UserID(id); ok {
		t.Fatal("UserID() accepted expired session")
	}
}
