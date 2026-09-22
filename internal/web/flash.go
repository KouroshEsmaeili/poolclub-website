package web

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const flashCookieName = "poolclub_flash"

type flashMessage struct {
	Category string
	Text     string
}

type flashStore struct {
	mu           sync.Mutex
	messages     map[string]flashMessage
	cookieSecure bool
}

func newFlashStore(cookieSecure bool) *flashStore {
	return &flashStore{messages: make(map[string]flashMessage), cookieSecure: cookieSecure}
}

func (s *flashStore) set(w http.ResponseWriter, message flashMessage) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	id := base64.RawURLEncoding.EncodeToString(random[:])
	s.mu.Lock()
	s.messages[id] = message
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: flashCookieName, Value: id, Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode})
}

func (s *flashStore) consume(w http.ResponseWriter, r *http.Request) *flashMessage {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	message, ok := s.messages[cookie.Value]
	delete(s.messages, cookie.Value)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: flashCookieName, Path: "/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode})
	if !ok {
		return nil
	}
	return &message
}
