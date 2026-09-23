package membership

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
)

const maxRequestBody = 1 << 20

// Authenticator resolves the current user from the existing session.
type Authenticator interface {
	Authenticate(http.ResponseWriter, *http.Request) (int64, bool)
}

// Handler serves authenticated membership endpoints.
type Handler struct {
	service       *Service
	authenticator Authenticator
}

// NewHandler creates a membership HTTP handler.
func NewHandler(service *Service, authenticator Authenticator) *Handler {
	return &Handler{service: service, authenticator: authenticator}
}

// RegisterRoutes adds membership routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/membership", h.get)
	mux.HandleFunc("POST /api/membership/buy", h.buy)
	mux.HandleFunc("POST /api/membership/cancel", h.cancel)
}

type purchaseRequest struct {
	PlanSlug string `json:"plan_slug"`
}

type cancelRequest struct {
	HistoryID string `json:"history_id"`
}

type currentResponse struct {
	Slug      *string `json:"slug"`
	Name      *string `json:"name"`
	ExpiresAt *string `json:"expires_at"`
	Active    bool    `json:"active"`
}

type historyResponse struct {
	ID          string    `json:"id"`
	PlanSlug    string    `json:"plan_slug"`
	PlanName    string    `json:"plan_name"`
	PurchasedAt time.Time `json:"purchased_at"`
	ExpiresAt   string    `json:"expires_at"`
	Amount      int64     `json:"amount"`
	Status      string    `json:"status"`
	CanCancel   bool      `json:"can_cancel"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	plans, err := h.service.Plans()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری طرح‌های اشتراک.")
		return
	}
	current, err := h.service.CurrentMembership(r.Context(), userID)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "خطا در دریافت وضعیت اشتراک.")
		return
	}
	history, err := h.service.History(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "خطا در دریافت سوابق اشتراک.")
		return
	}

	now := h.service.now().In(h.service.location)
	historyPayload := make([]historyResponse, 0, len(history))
	for _, item := range history {
		historyPayload = append(historyPayload, newHistoryResponse(item, now, h.service.location))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "success",
		"membership":     newCurrentResponse(current),
		"wallet_balance": current.WalletBalance,
		"plans":          plans,
		"history":        historyPayload,
	})
}

func (h *Handler) buy(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request purchaseRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "درخواست نامعتبر است.")
		return
	}
	request.PlanSlug = strings.TrimSpace(request.PlanSlug)
	if request.PlanSlug == "" {
		writeError(w, http.StatusBadRequest, "شناسه طرح اشتراک ارسال نشده است.")
		return
	}

	result, err := h.service.Purchase(r.Context(), userID, request.PlanSlug)
	switch {
	case errors.Is(err, ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "طرح اشتراک انتخاب‌شده یافت نشد.")
		return
	case errors.Is(err, ErrInvalidPlan):
		writeError(w, http.StatusBadRequest, "طرح اشتراک نامعتبر است.")
		return
	case errors.Is(err, ErrPlansUnavailable):
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری طرح‌های اشتراک.")
		return
	case errors.Is(err, ErrInsufficientFunds):
		writeError(w, http.StatusPaymentRequired, "موجودی کیف پول برای خرید این اشتراک کافی نیست.")
		return
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "خطا در خرید اشتراک.")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "success",
		"message": "اشتراک «" + result.Membership.PlanName + "» با موفقیت فعال شد. اعتبار تا " +
			result.Membership.ExpiresAt.Format("2006-01-02") + ".",
		"membership":   newCurrentResponse(result.Current),
		"history_item": newHistoryResponse(result.Membership, h.service.now(), h.service.location),
		"new_balance":  result.NewBalance,
	})
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request cancelRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "درخواست نامعتبر است.")
		return
	}
	request.HistoryID = strings.TrimSpace(request.HistoryID)
	if request.HistoryID == "" {
		writeError(w, http.StatusBadRequest, "شناسه اشتراک ارسال نشده است.")
		return
	}

	result, err := h.service.Cancel(r.Context(), userID, request.HistoryID)
	switch {
	case errors.Is(err, ErrMembershipNotFound):
		writeError(w, http.StatusNotFound, "اشتراک مورد نظر یافت نشد.")
		return
	case errors.Is(err, ErrMembershipInactive):
		writeError(w, http.StatusConflict, "این اشتراک در حال حاضر فعال نیست.")
		return
	case errors.Is(err, ErrCancellationWindow):
		writeError(w, http.StatusConflict, "امکان لغو اشتراک فقط در روز خرید وجود دارد.")
		return
	case errors.Is(err, ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "خطا در لغو اشتراک.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "success",
		"message":     "اشتراک با موفقیت لغو شد و مبلغ به کیف پول بازگشت.",
		"history_id":  result.Membership.ID,
		"new_balance": result.NewBalance,
	})
}

func newCurrentResponse(current Current) currentResponse {
	var expiresAt *string
	if current.ExpiresAt != nil {
		formatted := current.ExpiresAt.Format("2006-01-02")
		expiresAt = &formatted
	}
	return currentResponse{
		Slug:      current.Slug,
		Name:      current.Name,
		ExpiresAt: expiresAt,
		Active:    current.Active,
	}
}

func newHistoryResponse(item model.MembershipHistoryItem, now time.Time, location *time.Location) historyResponse {
	return historyResponse{
		ID:          item.ID,
		PlanSlug:    item.PlanSlug,
		PlanName:    item.PlanName,
		PurchasedAt: item.PurchasedAt,
		ExpiresAt:   item.ExpiresAt.Format("2006-01-02"),
		Amount:      item.Amount,
		Status:      item.Status,
		CanCancel: item.Status == StatusActive &&
			item.PurchasedAt.In(location).Format("2006-01-02") == now.In(location).Format("2006-01-02"),
	}
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
