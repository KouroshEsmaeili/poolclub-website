package wallet_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
)

func TestWalletEndpointsRequireAuthenticationAndReturnWallet(t *testing.T) {
	db := migratedWalletDatabase(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	walletHandler := wallet.NewHandler(wallet.NewService(db), authHandler)
	server := httptest.NewServer(httpapi.NewRouter(authHandler, walletHandler))
	t.Cleanup(server.Close)

	unauthenticated := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/wallet", nil)
	if unauthenticated.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated wallet status = %d, want %d", unauthenticated.status, http.StatusUnauthorized)
	}
	unauthenticatedDeposit := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 1})
	if unauthenticatedDeposit.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deposit status = %d, want %d", unauthenticatedDeposit.status, http.StatusUnauthorized)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	registered := requestJSON(t, client, http.MethodPost, server.URL+"/api/auth/register", map[string]any{
		"email":     "wallet@example.com",
		"password":  "correct horse battery staple",
		"password2": "correct horse battery staple",
	})
	if registered.status != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %v", registered.status, http.StatusCreated, registered.body)
	}

	emptyWallet := requestJSON(t, client, http.MethodGet, server.URL+"/api/wallet", nil)
	if emptyWallet.status != http.StatusOK {
		t.Fatalf("wallet status = %d, want %d; body = %v", emptyWallet.status, http.StatusOK, emptyWallet.body)
	}
	if emptyWallet.body["balance"] != float64(0) {
		t.Fatalf("initial balance = %v, want 0", emptyWallet.body["balance"])
	}
	if transactions := emptyWallet.body["transactions"].([]any); len(transactions) != 0 {
		t.Fatalf("initial transaction count = %d, want 0", len(transactions))
	}

	invalid := requestJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 0})
	if invalid.status != http.StatusBadRequest {
		t.Fatalf("invalid deposit status = %d, want %d", invalid.status, http.StatusBadRequest)
	}

	deposit := requestJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 50000})
	if deposit.status != http.StatusOK {
		t.Fatalf("deposit status = %d, want %d; body = %v", deposit.status, http.StatusOK, deposit.body)
	}
	if deposit.body["status"] != "success" || deposit.body["new_balance"] != float64(50000) {
		t.Fatalf("deposit body = %v, want success and balance 50000", deposit.body)
	}

	loaded := requestJSON(t, client, http.MethodGet, server.URL+"/api/wallet", nil)
	if loaded.status != http.StatusOK {
		t.Fatalf("loaded wallet status = %d, want %d; body = %v", loaded.status, http.StatusOK, loaded.body)
	}
	if loaded.body["balance"] != float64(50000) {
		t.Fatalf("loaded balance = %v, want 50000", loaded.body["balance"])
	}
	transactions := loaded.body["transactions"].([]any)
	if len(transactions) != 1 {
		t.Fatalf("loaded transaction count = %d, want 1", len(transactions))
	}
	transaction := transactions[0].(map[string]any)
	if transaction["amount"] != float64(50000) || transaction["type"] != wallet.TransactionTypeDeposit {
		t.Fatalf("loaded transaction = %v, want 50000 deposit", transaction)
	}
	if transaction["description"] != wallet.ManualDepositDescription {
		t.Fatalf("loaded description = %v, want %q", transaction["description"], wallet.ManualDepositDescription)
	}

	stringDeposit := requestJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": "25000"})
	if stringDeposit.status != http.StatusOK || stringDeposit.body["new_balance"] != float64(75000) {
		t.Fatalf("string deposit response = status %d, body %v; want 200 and balance 75000", stringDeposit.status, stringDeposit.body)
	}

	var storedTransactions int64
	if err := db.Model(&model.WalletTransaction{}).Count(&storedTransactions).Error; err != nil {
		t.Fatalf("count stored transactions: %v", err)
	}
	if storedTransactions != 2 {
		t.Fatalf("stored transaction count = %d, want 2", storedTransactions)
	}
}

type endpointResponse struct {
	status int
	body   map[string]any
}

func requestJSON(t *testing.T, client *http.Client, method string, url string, payload any) endpointResponse {
	t.Helper()
	var requestBody io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, url, requestBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()

	body := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return endpointResponse{status: response.StatusCode, body: body}
}
