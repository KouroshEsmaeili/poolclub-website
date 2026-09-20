package auth

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

type session struct {
	userID    int64
	expiresAt time.Time
}

// SessionStore keeps opaque session identifiers on the server.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
	ttl      time.Duration
	now      func() time.Time
}

// NewSessionStore creates an in-memory server-side session store.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]session),
		ttl:      ttl,
		now:      time.Now,
	}
}

// Create generates and stores a new session for a user.
func (s *SessionStore) Create(userID int64) (string, time.Time, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", time.Time{}, err
	}

	id := base64.RawURLEncoding.EncodeToString(random)
	expiresAt := s.now().Add(s.ttl)

	s.mu.Lock()
	s.sessions[id] = session{userID: userID, expiresAt: expiresAt}
	s.mu.Unlock()

	return id, expiresAt, nil
}

// UserID returns the user for a valid, unexpired session.
func (s *SessionStore) UserID(id string) (int64, bool) {
	if id == "" {
		return 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.sessions[id]
	if !ok {
		return 0, false
	}
	if !stored.expiresAt.After(s.now()) {
		delete(s.sessions, id)
		return 0, false
	}

	return stored.userID, true
}

// Delete invalidates a session immediately.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}
