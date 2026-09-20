package booking

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
)

const maxRequestBody = 1 << 20

// Authenticator resolves the current user from the existing session.
type Authenticator interface {
	Authenticate(http.ResponseWriter, *http.Request) (int64, bool)
}

// Handler serves the Flask-compatible booking endpoints.
type Handler struct {
	service       *Service
	authenticator Authenticator
}

// NewHandler creates a booking HTTP handler.
func NewHandler(service *Service, authenticator Authenticator) *Handler {
	return &Handler{service: service, authenticator: authenticator}
}

// RegisterRoutes adds booking routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/bookings/create", h.create)
	mux.HandleFunc("POST /api/bookings/cancel", h.cancel)
}

type createRequest struct {
	Date     string          `json:"date"`
	Time     string          `json:"time"`
	Duration json.RawMessage `json:"duration"`
	Type     string          `json:"type"`
}

type cancelRequest struct {
	BookingID json.RawMessage `json:"booking_id"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request createRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeMissingFields(w)
		return
	}
	request.Type = strings.TrimSpace(request.Type)
	if request.Date == "" || request.Time == "" || len(request.Duration) == 0 || request.Type == "" || string(request.Duration) == "null" {
		writeMissingFields(w)
		return
	}
	duration, err := parseInteger(request.Duration)
	if err != nil || duration <= 0 {
		writeAPIError(w, http.StatusBadRequest, "مدت سانس نامعتبر است.")
		return
	}

	result, err := h.service.Create(r.Context(), userID, CreateInput{
		Date:     request.Date,
		Time:     request.Time,
		Duration: duration,
		Type:     request.Type,
	})
	if err != nil {
		h.writeCreateError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status":      "success",
		"message":     "رزرو با موفقیت ثبت شد.",
		"booking_id":  result.Booking.ID,
		"lane":        result.Booking.Lane,
		"new_balance": result.NewBalance,
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request cancelRequest
	if err := decodeJSON(w, r, &request); err != nil || isMissingID(request.BookingID) {
		writeAPIError(w, http.StatusBadRequest, "شناسه رزرو ارسال نشده است.")
		return
	}
	bookingID, err := parseInteger(request.BookingID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "رزرو یافت نشد.")
		return
	}
	if err := h.service.Cancel(r.Context(), userID, bookingID); errors.Is(err, ErrBookingNotFound) {
		writeAPIError(w, http.StatusNotFound, "رزرو یافت نشد.")
		return
	} else if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "خطا در لغو رزرو.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "رزرو لغو شد.",
	})
}

func (h *Handler) writeCreateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidDuration):
		writeAPIError(w, http.StatusBadRequest, "مدت سانس نامعتبر است.")
	case errors.Is(err, ErrInvalidType):
		writeMissingFields(w)
	case errors.Is(err, ErrInvalidDate), errors.Is(err, ErrInvalidTime), errors.Is(err, ErrPastBooking):
		writeAPIError(w, http.StatusBadRequest, "امکان ثبت رزرو برای زمان گذشته وجود ندارد.")
	case errors.Is(err, ErrOverlap):
		writeAPIError(w, http.StatusConflict, "شما در این بازه زمانی رزرو دیگری دارید.")
	case errors.Is(err, ErrPoolCapacity):
		writeAPIError(w, http.StatusConflict, "ظرفیت استخر برای این بازه زمانی تکمیل است.")
	case errors.Is(err, ErrNoLane):
		writeAPIError(w, http.StatusConflict, "تمام لاین‌های تمرینی در این بازه زمانی پر هستند.")
	case errors.Is(err, wallet.ErrInsufficientFunds):
		writeAPIError(w, http.StatusPaymentRequired, "موجودی کیف پول برای این رزرو کافی نیست.")
	case errors.Is(err, wallet.ErrUserNotFound):
		writeAPIError(w, http.StatusUnauthorized, "authentication required")
	default:
		writeAPIError(w, http.StatusInternalServerError, "خطا در ثبت رزرو.")
	}
}

func writeMissingFields(w http.ResponseWriter) {
	writeAPIError(w, http.StatusBadRequest, "لطفاً تمام فیلدهای مورد نیاز (تاریخ، ساعت، مدت و نوع رزرو) را پر کنید.")
}

func isMissingID(raw json.RawMessage) bool {
	value := strings.TrimSpace(string(raw))
	return value == "" || value == "null" || value == "0" || value == `""`
}

func parseInteger(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
	}
	return strconv.ParseInt(value, 10, 64)
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

func writeAPIError(w http.ResponseWriter, status int, message string) {
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
