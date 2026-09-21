package classes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

func TestClassEnrollmentEndpointRequiresAuthentication(t *testing.T) {
	_, server := classHTTPEnvironment(t, classTestCatalog())
	response := requestClassJSON(t, server.Client(), http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "beginner"})
	if response.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.status)
	}
}

func TestClassEnrollmentManualFlowMatchesFrontendAndFlask(t *testing.T) {
	db, server := classHTTPEnvironment(t, classTestCatalog())
	client := classClient(t, server)
	registerClassUser(t, client, server.URL, "class-http@example.com")

	malformed := requestRawClass(t, client, http.MethodPost, server.URL+"/api/classes/enroll", []byte(`{"class_slug":`))
	if malformed.status != http.StatusBadRequest {
		t.Fatalf("malformed status = %d, want 400", malformed.status)
	}
	missing := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{})
	if missing.status != http.StatusBadRequest {
		t.Fatalf("missing-slug status = %d, want 400", missing.status)
	}
	unknown := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "missing"})
	if unknown.status != http.StatusNotFound {
		t.Fatalf("unknown-class status = %d, want 404", unknown.status)
	}
	invalidPrice := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "invalid-price"})
	if invalidPrice.status != http.StatusBadRequest {
		t.Fatalf("invalid-price status = %d, want 400", invalidPrice.status)
	}

	deposit := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 30000})
	if deposit.status != http.StatusOK || deposit.body["new_balance"] != float64(30000) {
		t.Fatalf("deposit = status %d body %v", deposit.status, deposit.body)
	}
	enrolled := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "beginner"})
	if enrolled.status != http.StatusCreated || enrolled.body["status"] != "success" || enrolled.body["new_balance"] != float64(0) {
		t.Fatalf("enrollment = status %d body %v", enrolled.status, enrolled.body)
	}
	enrollmentID, ok := enrolled.body["enrollment_id"].(string)
	if !ok || len(enrollmentID) != 36 {
		t.Fatalf("enrollment ID = %v, want UUID", enrolled.body["enrollment_id"])
	}
	if enrolled.body["message"] != "ثبت‌نام در کلاس «مبتدی» با موفقیت انجام شد." {
		t.Fatalf("success message = %v", enrolled.body["message"])
	}

	member, err := user.NewStore(db).FindByEmail(t.Context(), "class-http@example.com")
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	stored := loadEnrollment(t, db, enrollmentID)
	if stored.ClassSlug != "beginner" || stored.Price != 30000 || stored.Status != StatusActive {
		t.Fatalf("stored enrollment = %+v", stored)
	}
	transaction := onlyClassWalletTransactionOfType(t, db, member.ID, wallet.TransactionTypePurchase)
	if transaction.Amount != -30000 || transaction.Type != wallet.TransactionTypePurchase || valueOrEmpty(transaction.Description) != "ثبت‌نام در کلاس: مبتدی" {
		t.Fatalf("purchase transaction = %+v", transaction)
	}

	duplicateWithoutFunds := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "beginner"})
	if duplicateWithoutFunds.status != http.StatusPaymentRequired {
		t.Fatalf("duplicate without funds = status %d body %v, want 402", duplicateWithoutFunds.status, duplicateWithoutFunds.body)
	}
	assertClassEnrollmentCount(t, db, member.ID, 1)
	assertClassWalletTransactionTypeCount(t, db, member.ID, wallet.TransactionTypePurchase, 1)
	assertClassWalletTransactionCount(t, db, member.ID, 2)
}

func TestUnavailableClassCatalogReturnsSafeServerError(t *testing.T) {
	_, server := classHTTPEnvironment(t, LoadCatalog(filepath.Join(t.TempDir(), "missing.json")))
	client := classClient(t, server)
	registerClassUser(t, client, server.URL, "missing-config@example.com")

	response := requestClassJSON(t, client, http.MethodPost, server.URL+"/api/classes/enroll", map[string]any{"class_slug": "beginner"})
	if response.status != http.StatusInternalServerError || response.body["status"] != "error" {
		t.Fatalf("missing catalog response = status %d body %v, want safe 500", response.status, response.body)
	}
}

func classHTTPEnvironment(t *testing.T, catalog Catalog) (*gorm.DB, *httptest.Server) {
	t.Helper()
	db, _ := classTestEnvironment(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	walletService := wallet.NewService(db)
	service := NewService(db, walletService, catalog)
	service.now = func() time.Time { return classNow }
	server := httptest.NewServer(httpapi.NewRouter(
		authHandler,
		wallet.NewHandler(walletService, authHandler),
		NewHandler(service, authHandler),
	))
	t.Cleanup(server.Close)
	return db, server
}

func classTestCatalog() Catalog {
	coach := "مربی یک"
	classTime := "شنبه ۱۰"
	return NewCatalog([]Definition{
		{Slug: "beginner", Name: "مبتدی", Coach: &coach, Time: &classTime, PriceAmount: 30000, Capacity: 1},
		{Slug: "invalid-price", Name: "نامعتبر", PriceAmount: 0},
	})
}

type classEndpointResponse struct {
	status int
	body   map[string]any
}

func classClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := *server.Client()
	client.Jar = jar
	return &client
}

func registerClassUser(t *testing.T, client *http.Client, serverURL string, email string) {
	t.Helper()
	response := requestClassJSON(t, client, http.MethodPost, serverURL+"/api/auth/register", map[string]any{
		"email": email, "password": "correct horse battery staple", "password2": "correct horse battery staple",
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d body %v, want 201", response.status, response.body)
	}
}

func requestClassJSON(t *testing.T, client *http.Client, method string, url string, payload any) classEndpointResponse {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	return sendClassRequest(t, client, method, url, body)
}

func requestRawClass(t *testing.T, client *http.Client, method string, url string, payload []byte) classEndpointResponse {
	t.Helper()
	return sendClassRequest(t, client, method, url, bytes.NewReader(payload))
}

func sendClassRequest(t *testing.T, client *http.Client, method string, url string, body io.Reader) classEndpointResponse {
	t.Helper()
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()
	payload := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return classEndpointResponse{status: response.StatusCode, body: payload}
}
