package web

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const (
	flashCookieName = "poolclub_flash"
	flashTTL        = 10 * time.Minute
)

type flashMessage struct {
	Category string
	Text     string
}

type storedFlash struct {
	message   flashMessage
	expiresAt time.Time
}

type flashStore struct {
	mu           sync.Mutex
	messages     map[string]storedFlash
	cookieSecure bool
	now          func() time.Time
}

func newFlashStore(cookieSecure bool) *flashStore {
	return &flashStore{
		messages:     make(map[string]storedFlash),
		cookieSecure: cookieSecure,
		now:          time.Now,
	}
}

func (s *flashStore) set(w http.ResponseWriter, message flashMessage) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	id := base64.RawURLEncoding.EncodeToString(random[:])
	now := s.now()
	expiresAt := now.Add(flashTTL)

	s.mu.Lock()
	s.pruneExpiredLocked(now)
	s.messages[id] = storedFlash{message: message, expiresAt: expiresAt}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(flashTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *flashStore) consume(w http.ResponseWriter, r *http.Request) *flashMessage {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}

	now := s.now()
	s.mu.Lock()
	s.pruneExpiredLocked(now)
	stored, ok := s.messages[cookie.Value]
	delete(s.messages, cookie.Value)
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(1, 0),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	if !ok {
		return nil
	}
	message := stored.message
	return &message
}

func (s *flashStore) pruneExpiredLocked(now time.Time) {
	for id, stored := range s.messages {
		if !stored.expiresAt.After(now) {
			delete(s.messages, id)
		}
	}
}
