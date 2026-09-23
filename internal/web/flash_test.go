package web

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestFlashStoreConsumesAndExpiresMessages(t *testing.T) {
	now := time.Date(2026, time.September, 23, 8, 0, 0, 0, time.UTC)
	store := newFlashStore(false)
	store.now = func() time.Time { return now }

	writer := httptest.NewRecorder()
	store.set(writer, flashMessage{Category: "success", Text: "saved"})
	cookies := writer.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("set cookies = %d, want 1", len(cookies))
	}

	request := httptest.NewRequest("GET", "http://example.test/", nil)
	request.AddCookie(cookies[0])
	message := store.consume(httptest.NewRecorder(), request)
	if message == nil || message.Category != "success" || message.Text != "saved" {
		t.Fatalf("consume() = %#v", message)
	}
	if got := len(store.messages); got != 0 {
		t.Fatalf("stored messages after consume = %d, want 0", got)
	}

	writer = httptest.NewRecorder()
	store.set(writer, flashMessage{Category: "info", Text: "temporary"})
	cookies = writer.Result().Cookies()
	now = now.Add(flashTTL + time.Second)
	request = httptest.NewRequest("GET", "http://example.test/", nil)
	request.AddCookie(cookies[0])
	if message := store.consume(httptest.NewRecorder(), request); message != nil {
		t.Fatalf("expired message = %#v, want nil", message)
	}
	if got := len(store.messages); got != 0 {
		t.Fatalf("stored messages after expiration = %d, want 0", got)
	}
}

func TestFlashStorePrunesAbandonedExpiredMessages(t *testing.T) {
	now := time.Date(2026, time.September, 23, 8, 0, 0, 0, time.UTC)
	store := newFlashStore(false)
	store.now = func() time.Time { return now }

	store.set(httptest.NewRecorder(), flashMessage{Category: "info", Text: "old"})
	if got := len(store.messages); got != 1 {
		t.Fatalf("stored messages = %d, want 1", got)
	}

	now = now.Add(flashTTL + time.Second)
	store.set(httptest.NewRecorder(), flashMessage{Category: "success", Text: "new"})
	if got := len(store.messages); got != 1 {
		t.Fatalf("stored messages after prune = %d, want 1", got)
	}
}
