package wallet

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
)

const maxRequestBody = 1 << 20

// Authenticator resolves the current user from the existing session.
type Authenticator interface {
	Authenticate(http.ResponseWriter, *http.Request) (int64, bool)
}

// Handler serves the JSON wallet endpoints.
type Handler struct {
	service       *Service
	authenticator Authenticator
}

// NewHandler creates a wallet HTTP handler.
func NewHandler(service *Service, authenticator Authenticator) *Handler {
	return &Handler{service: service, authenticator: authenticator}
}

// RegisterRoutes adds wallet routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/wallet", h.get)
	mux.HandleFunc("POST /api/wallet/deposit", h.deposit)
}

type depositRequest struct {
	Amount json.RawMessage `json:"amount"`
}

type transactionResponse struct {
	ID          int64     `json:"id"`
	Amount      int64     `json:"amount"`
	Type        string    `json:"type"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	snapshot, err := h.service.Get(r.Context(), userID)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load wallet")
		return
	}

	transactions := make([]transactionResponse, 0, len(snapshot.Transactions))
	for _, transaction := range snapshot.Transactions {
		transactions = append(transactions, newTransactionResponse(transaction))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"balance":      snapshot.Balance,
		"transactions": transactions,
	})
}

func (h *Handler) deposit(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.authenticator.Authenticate(w, r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var request depositRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeInvalidAmount(w)
		return
	}
	amount, amountErr := parseAmount(request.Amount)
	if amountErr != nil || amount <= 0 {
		writeInvalidAmount(w)
		return
	}

	balance, err := h.service.Deposit(r.Context(), userID, amount, ManualDepositDescription)
	if errors.Is(err, ErrUserNotFound) {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not deposit funds")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "success",
		"message":     "کیف پول با موفقیت شارژ شد.",
		"new_balance": balance,
	})
}

func writeInvalidAmount(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"status":  "error",
		"message": "مبلغ نامعتبر است.",
	})
}

func parseAmount(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, err
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func newTransactionResponse(transaction model.WalletTransaction) transactionResponse {
	description := ""
	if transaction.Description != nil {
		description = *transaction.Description
	}
	return transactionResponse{
		ID:          transaction.ID,
		Amount:      transaction.Amount,
		Type:        transaction.Type,
		Timestamp:   transaction.Timestamp,
		Description: description,
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
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
