package classes

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxRequestBody = 1 << 20

// Authenticator resolves the current user from the existing session.
type Authenticator interface {
	Authenticate(http.ResponseWriter, *http.Request) (int64, bool)
}

// Handler serves the Flask-compatible class enrollment endpoint.
type Handler struct {
	service       *Service
	authenticator Authenticator
}

// NewHandler creates a class HTTP handler.
func NewHandler(service *Service, authenticator Authenticator) *Handler {
	return &Handler{service: service, authenticator: authenticator}
}

// RegisterRoutes adds class routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/classes/enroll", h.enroll)
}

type enrollRequest struct {
	ClassSlug string `json:"class_slug"`
}

func (h *Handler) enroll(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request enrollRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "کلاس نامعتبر است.")
		return
	}
	request.ClassSlug = strings.TrimSpace(request.ClassSlug)
	if request.ClassSlug == "" {
		writeError(w, http.StatusBadRequest, "کلاس نامعتبر است.")
		return
	}

	result, err := h.service.Enroll(r.Context(), userID, request.ClassSlug)
	switch {
	case errors.Is(err, ErrClassNotFound):
		writeError(w, http.StatusNotFound, "کلاس مورد نظر یافت نشد.")
		return
	case errors.Is(err, ErrInvalidPrice):
		writeError(w, http.StatusBadRequest, "قیمت کلاس نامعتبر است.")
		return
	case errors.Is(err, ErrCatalogUnavailable):
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری اطلاعات کلاس‌ها.")
		return
	case errors.Is(err, ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "موجودی کیف پول برای ثبت‌نام در این کلاس کافی نیست.")
		return
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "خطا در ثبت‌نام کلاس.")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":        "success",
		"message":       "ثبت‌نام در کلاس «" + result.Enrollment.ClassName + "» با موفقیت انجام شد.",
		"enrollment_id": result.Enrollment.ID,
		"new_balance":   result.NewBalance,
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
