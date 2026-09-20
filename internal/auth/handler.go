package auth

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookieName = "poolclub_session"
	maxRequestBody    = 1 << 20
)

// Handler serves the JSON authentication endpoints.
type Handler struct {
	users        *user.Store
	sessions     *SessionStore
	cookieSecure bool
	dummyHash    string
}

// NewHandler creates an authentication handler.
func NewHandler(users *user.Store, sessions *SessionStore, cookieSecure bool) (*Handler, error) {
	dummyHash, err := HashPassword("invalid-password-placeholder")
	if err != nil {
		return nil, err
	}

	return &Handler{
		users:        users,
		sessions:     sessions,
		cookieSecure: cookieSecure,
		dummyHash:    dummyHash,
	}, nil
}

// RegisterRoutes adds authentication routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", h.register)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", h.me)
}

type registerRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Password  string `json:"password"`
	Password2 string `json:"password2"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}

	request.FirstName = strings.TrimSpace(request.FirstName)
	request.LastName = strings.TrimSpace(request.LastName)
	request.Email = user.NormalizeEmail(request.Email)

	if request.Email == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if request.Password != request.Password2 {
		writeError(w, http.StatusBadRequest, "password confirmation does not match")
		return
	}

	passwordHash, err := HashPassword(request.Password)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		writeError(w, http.StatusBadRequest, "password is too long")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	created, err := h.users.Create(
		r.Context(),
		request.Email,
		passwordHash,
		request.FirstName,
		request.LastName,
	)
	if errors.Is(err, user.ErrEmailExists) {
		writeError(w, http.StatusConflict, "email is already registered")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create account")
		return
	}

	if err := h.startSession(w, r, created.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"user": newUserResponse(created)})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON request")
		return
	}

	request.Email = user.NormalizeEmail(request.Email)
	found, err := h.users.FindByEmail(r.Context(), request.Email)
	if errors.Is(err, user.ErrNotFound) {
		VerifyPassword(h.dummyHash, request.Password)
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not log in")
		return
	}

	valid, needsRehash := VerifyPassword(found.PasswordHash, request.Password)
	if !valid {
		writeError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if needsRehash {
		passwordHash, err := HashPassword(request.Password)
		if err != nil || h.users.UpdatePasswordHash(r.Context(), found.ID, passwordHash) != nil {
			writeError(w, http.StatusInternalServerError, "could not log in")
			return
		}
	}

	if err := h.startSession(w, r, found.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": newUserResponse(found)})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.sessions.Delete(cookie.Value)
	}
	h.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Authenticate returns the user ID for a valid session cookie.
func (h *Handler) Authenticate(w http.ResponseWriter, r *http.Request) (int64, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return 0, false
	}

	userID, ok := h.sessions.UserID(cookie.Value)
	if !ok {
		h.clearSessionCookie(w)
		return 0, false
	}
	return userID, true
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	found, err := h.users.FindByID(r.Context(), userID)
	if errors.Is(err, user.ErrNotFound) {
		if cookie, cookieErr := r.Cookie(sessionCookieName); cookieErr == nil {
			h.sessions.Delete(cookie.Value)
		}
		h.clearSessionCookie(w)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load user")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"user": newUserResponse(found)})
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.sessions.Delete(cookie.Value)
	}

	id, expiresAt, err := h.sessions.Create(userID)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func newUserResponse(found *model.User) userResponse {
	return userResponse{
		ID:        found.ID,
		Email:     found.Email,
		FirstName: valueOrEmpty(found.FirstName),
		LastName:  valueOrEmpty(found.LastName),
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
