package events

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxRequestBody = 1 << 20

// Authenticator resolves an optional or required current session.
type Authenticator interface {
	Authenticate(http.ResponseWriter, *http.Request) (int64, bool)
}

// Handler serves the Flask-compatible event registration endpoints.
type Handler struct {
	service       *Service
	authenticator Authenticator
}

// NewHandler creates an event HTTP handler.
func NewHandler(service *Service, authenticator Authenticator) *Handler {
	return &Handler{service: service, authenticator: authenticator}
}

// RegisterRoutes adds event registration routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/events/public-register", h.publicRegister)
	mux.HandleFunc("POST /api/events/register", h.register)
}

type publicRegisterRequest struct {
	EventSlug string `json:"event_slug"`
	Name      string `json:"name"`
	Email     string `json:"email"`
}

type registerRequest struct {
	Slug string `json:"slug"`
}

func (h *Handler) publicRegister(w http.ResponseWriter, r *http.Request) {
	var request publicRegisterRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "رویداد نامعتبر است.")
		return
	}
	request.EventSlug = strings.TrimSpace(request.EventSlug)
	request.Name = strings.TrimSpace(request.Name)
	request.Email = strings.TrimSpace(request.Email)
	if request.EventSlug == "" {
		writeError(w, http.StatusBadRequest, "رویداد نامعتبر است.")
		return
	}

	var userID *int64
	if authenticatedUserID, ok := h.authenticator.Authenticate(w, r); ok {
		userID = &authenticatedUserID
	}
	registration, err := h.service.PublicRegister(
		r.Context(), userID, request.EventSlug, request.Name, request.Email,
	)
	switch {
	case errors.Is(err, ErrEventNotFound):
		writeError(w, http.StatusNotFound, "رویداد یافت نشد.")
		return
	case errors.Is(err, ErrIdentityRequired):
		writeError(w, http.StatusBadRequest, "لطفاً نام و ایمیل خود را وارد کنید.")
		return
	case errors.Is(err, ErrCatalogUnavailable):
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری اطلاعات رویدادها.")
		return
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "خطا در ثبت‌نام رویداد.")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":          "success",
		"message":         "ثبت‌نام شما برای رویداد «" + registration.Title + "» با موفقیت ثبت شد.",
		"registration_id": registration.ID,
	})
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request registerRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "رویداد نامعتبر است.")
		return
	}
	request.Slug = strings.TrimSpace(request.Slug)
	if request.Slug == "" {
		writeError(w, http.StatusBadRequest, "رویداد نامعتبر است.")
		return
	}

	result, err := h.service.Register(r.Context(), userID, request.Slug)
	switch {
	case errors.Is(err, ErrEventNotFound):
		writeError(w, http.StatusNotFound, "رویداد نامعتبر است.")
		return
	case errors.Is(err, ErrRegistrationClosed):
		writeError(w, http.StatusConflict, "ثبت‌نام این رویداد فعال نیست.")
		return
	case errors.Is(err, ErrCapacityReached):
		writeError(w, http.StatusConflict, "ظرفیت این رویداد تکمیل شده است.")
		return
	case errors.Is(err, ErrAlreadyRegistered):
		writeError(w, http.StatusConflict, "شما قبلاً در این رویداد ثبت‌نام کرده‌اید.")
		return
	case errors.Is(err, ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "موجودی کیف پول کافی نیست.")
		return
	case errors.Is(err, ErrCatalogUnavailable):
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری اطلاعات رویدادها.")
		return
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "خطا در ثبت‌نام رویداد.")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":           "success",
		"message":          "ثبت‌نام در رویداد با موفقیت انجام شد.",
		"registration_id":  result.Registration.ID,
		"new_balance":      result.NewBalance,
		"registered_count": result.RegisteredCount,
	})
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
	writeJSON(w, status, map[string]string{
		"status":  "error",
		"message": message,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
