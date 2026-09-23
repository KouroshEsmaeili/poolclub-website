package events

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
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"gorm.io/gorm"
)

func TestAuthenticatedEventEndpointRequiresAuthentication(t *testing.T) {
	_, server := eventHTTPEnvironment(t, eventHandlerCatalog())
	response := requestEventJSON(t, server.Client(), http.MethodPost, server.URL+"/api/events/register", map[string]any{"slug": "paid"})
	if response.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.status)
	}
}

func TestEventRegistrationManualFlowMatchesFrontendContract(t *testing.T) {
	db, server := eventHTTPEnvironment(t, eventHandlerCatalog())
	guestClient := server.Client()

	malformed := requestRawEvent(t, guestClient, http.MethodPost, server.URL+"/api/events/public-register", []byte(`{"event_slug":`))
	if malformed.status != http.StatusBadRequest {
		t.Fatalf("malformed public status = %d, want 400", malformed.status)
	}
	missingIdentity := requestEventJSON(t, guestClient, http.MethodPost, server.URL+"/api/events/public-register", map[string]any{"event_slug": "paid"})
	if missingIdentity.status != http.StatusBadRequest {
		t.Fatalf("missing identity status = %d body %v, want 400", missingIdentity.status, missingIdentity.body)
	}
	unknown := requestEventJSON(t, guestClient, http.MethodPost, server.URL+"/api/events/public-register", map[string]any{"event_slug": "missing", "name": "Guest", "email": "guest@example.com"})
	if unknown.status != http.StatusNotFound {
		t.Fatalf("unknown public event status = %d, want 404", unknown.status)
	}

	publicRegistration := requestEventJSON(t, guestClient, http.MethodPost, server.URL+"/api/events/public-register", map[string]any{
		"event_slug": "paid", "name": "Guest Person", "email": "guest@example.com",
	})
	if publicRegistration.status != http.StatusCreated || publicRegistration.body["status"] != "success" || publicRegistration.body["message"] != "ثبت‌نام شما برای رویداد «مسابقه» با موفقیت ثبت شد." {
		t.Fatalf("public registration = status %d body %v", publicRegistration.status, publicRegistration.body)
	}
	publicID := responseInt64(t, publicRegistration.body["registration_id"])
	storedPublic := loadEventRegistration(t, db, publicID)
	if storedPublic.UserID != nil || storedPublic.Price != 0 || valueOrEmpty(storedPublic.Name) != "Guest Person" || valueOrEmpty(storedPublic.Email) != "guest@example.com" {
		t.Fatalf("stored public registration = %+v", storedPublic)
	}

	client := eventClient(t, server)
	registerEventUser(t, client, server.URL, "event-http@example.com", "سارا", "کریمی")
	profileDefaulted := requestEventJSON(t, client, http.MethodPost, server.URL+"/api/events/public-register", map[string]any{"event_slug": "closed"})
	if profileDefaulted.status != http.StatusCreated {
		t.Fatalf("authenticated public registration = status %d body %v", profileDefaulted.status, profileDefaulted.body)
	}
	profileDefaultedID := responseInt64(t, profileDefaulted.body["registration_id"])
	profileRegistration := loadEventRegistration(t, db, profileDefaultedID)
	if profileRegistration.UserID == nil || valueOrEmpty(profileRegistration.Name) != "سارا کریمی" || valueOrEmpty(profileRegistration.Email) != "event-http@example.com" || profileRegistration.Price != 0 {
		t.Fatalf("profile-defaulted public registration = %+v", profileRegistration)
	}
	deposit := requestEventJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 30000})
	if deposit.status != http.StatusOK || deposit.body["new_balance"] != float64(30000) {
		t.Fatalf("deposit = status %d body %v", deposit.status, deposit.body)
	}

	registered := requestEventJSON(t, client, http.MethodPost, server.URL+"/api/events/register", map[string]any{"slug": "paid"})
	if registered.status != http.StatusCreated || registered.body["status"] != "success" || registered.body["new_balance"] != float64(0) || registered.body["registered_count"] != float64(2) {
		t.Fatalf("authenticated registration = status %d body %v", registered.status, registered.body)
	}
	registrationID := responseInt64(t, registered.body["registration_id"])
	member, err := user.NewStore(db).FindByEmail(t.Context(), "event-http@example.com")
	if err != nil {
		t.Fatalf("load registered user: %v", err)
	}
	stored := loadEventRegistration(t, db, registrationID)
	if stored.UserID == nil || *stored.UserID != member.ID || stored.EventSlug != "paid" || stored.Title != "مسابقه" || stored.Price != 30000 || stored.Status != StatusRegistered || valueOrEmpty(stored.Name) != "سارا کریمی" || valueOrEmpty(stored.Email) != member.Email {
		t.Fatalf("stored authenticated registration = %+v", stored)
	}
	purchase := onlyEventWalletTransactionOfType(t, db, member.ID, wallet.TransactionTypePurchase)
	if purchase.Amount != -30000 || valueOrEmpty(purchase.Description) != "ثبت‌نام رویداد: مسابقه" {
		t.Fatalf("purchase transaction = %+v", purchase)
	}

	duplicate := requestEventJSON(t, client, http.MethodPost, server.URL+"/api/events/register", map[string]any{"slug": "paid"})
	if duplicate.status != http.StatusConflict || duplicate.body["message"] != "شما قبلاً در این رویداد ثبت‌نام کرده‌اید." {
		t.Fatalf("duplicate = status %d body %v, want duplicate 409", duplicate.status, duplicate.body)
	}
	assertEventRegistrationCount(t, db, "paid", 2)
	assertEventWalletTransactionTypeCount(t, db, member.ID, wallet.TransactionTypePurchase, 1)
	assertEventWalletTransactionCount(t, db, member.ID, 2)
}

func TestAuthenticatedEventEndpointMapsClosedCapacityInsufficientAndMalformed(t *testing.T) {
	_, server := eventHTTPEnvironment(t, eventHandlerCatalog())
	client := eventClient(t, server)
	registerEventUser(t, client, server.URL, "event-errors@example.com", "Error", "User")

	tests := []struct {
		name    string
		payload []byte
		status  int
		message string
	}{
		{name: "malformed", payload: []byte(`{"slug":`), status: http.StatusBadRequest, message: "رویداد نامعتبر است."},
		{name: "unknown", payload: mustEventJSON(t, map[string]any{"slug": "missing"}), status: http.StatusNotFound, message: "رویداد نامعتبر است."},
		{name: "closed", payload: mustEventJSON(t, map[string]any{"slug": "closed"}), status: http.StatusConflict, message: "ثبت‌نام این رویداد فعال نیست."},
		{name: "full", payload: mustEventJSON(t, map[string]any{"slug": "full"}), status: http.StatusConflict, message: "ظرفیت این رویداد تکمیل شده است."},
		{name: "insufficient", payload: mustEventJSON(t, map[string]any{"slug": "paid"}), status: http.StatusPaymentRequired, message: "موجودی کیف پول کافی نیست."},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestRawEvent(t, client, http.MethodPost, server.URL+"/api/events/register", test.payload)
			if response.status != test.status || response.body["message"] != test.message {
				t.Fatalf("response = status %d body %v, want %d/%q", response.status, response.body, test.status, test.message)
			}
		})
	}
}

func TestUnavailableEventCatalogReturnsSafeServerErrors(t *testing.T) {
	_, server := eventHTTPEnvironment(t, LoadCatalog(filepath.Join(t.TempDir(), "missing.json")))
	guest := requestEventJSON(t, server.Client(), http.MethodPost, server.URL+"/api/events/public-register", map[string]any{
		"event_slug": "paid", "name": "Guest", "email": "guest@example.com",
	})
	if guest.status != http.StatusInternalServerError || guest.body["status"] != "error" {
		t.Fatalf("public missing-catalog response = status %d body %v", guest.status, guest.body)
	}

	client := eventClient(t, server)
	registerEventUser(t, client, server.URL, "missing-events@example.com", "Missing", "Events")
	authenticated := requestEventJSON(t, client, http.MethodPost, server.URL+"/api/events/register", map[string]any{"slug": "paid"})
	if authenticated.status != http.StatusInternalServerError || authenticated.body["status"] != "error" {
		t.Fatalf("authenticated missing-catalog response = status %d body %v", authenticated.status, authenticated.body)
	}
}

func eventHTTPEnvironment(t *testing.T, catalog Catalog) (*gorm.DB, *httptest.Server) {
	t.Helper()
	db, _ := eventTestEnvironment(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	walletService := wallet.NewService(db)
	eventService := NewService(db, walletService, catalog)
	eventService.now = func() time.Time { return eventNow }
	server := httptest.NewServer(httpapi.NewRouter(
		authHandler,
		wallet.NewHandler(walletService, authHandler),
		NewHandler(eventService, authHandler),
	))
	t.Cleanup(server.Close)
	return db, server
}

func eventHandlerCatalog() Catalog {
	return NewCatalog([]Definition{
		{Slug: "paid", Title: "مسابقه", State: "open", Price: 30000, Capacity: eventInt64(5)},
		{Slug: "closed", Title: "بسته", State: "closed", Price: 100},
		{Slug: "full", Title: "پر", State: "open", Price: 100, Capacity: eventInt64(0)},
	})
}

type eventEndpointResponse struct {
	status int
	body   map[string]any
}

func eventClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := *server.Client()
	client.Jar = jar
	return &client
}

func registerEventUser(t *testing.T, client *http.Client, serverURL string, email string, firstName string, lastName string) {
	t.Helper()
	response := requestEventJSON(t, client, http.MethodPost, serverURL+"/api/auth/register", map[string]any{
		"email": email, "password": "correct horse battery staple", "password2": "correct horse battery staple",
		"first_name": firstName, "last_name": lastName,
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d body %v, want 201", response.status, response.body)
	}
}

func requestEventJSON(t *testing.T, client *http.Client, method string, url string, payload any) eventEndpointResponse {
	t.Helper()
	return requestRawEvent(t, client, method, url, mustEventJSON(t, payload))
}

func mustEventJSON(t *testing.T, payload any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}
	return encoded
}

func requestRawEvent(t *testing.T, client *http.Client, method string, url string, payload []byte) eventEndpointResponse {
	t.Helper()
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
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
	decoded := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return eventEndpointResponse{status: response.StatusCode, body: decoded}
}

func responseInt64(t *testing.T, value any) int64 {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("response integer = %T(%v), want JSON number", value, value)
	}
	return int64(number)
}

func onlyEventWalletTransactionOfType(t *testing.T, db *gorm.DB, userID int64, transactionType string) model.WalletTransaction {
	t.Helper()
	var transactions []model.WalletTransaction
	if err := db.Where("user_id = ? AND type = ?", userID, transactionType).Find(&transactions).Error; err != nil {
		t.Fatalf("load wallet transactions: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("wallet %q transaction count = %d, want 1", transactionType, len(transactions))
	}
	return transactions[0]
}

func assertEventWalletTransactionTypeCount(t *testing.T, db *gorm.DB, userID int64, transactionType string, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.WalletTransaction{}).Where("user_id = ? AND type = ?", userID, transactionType).Count(&count).Error; err != nil {
		t.Fatalf("count wallet transactions: %v", err)
	}
	if count != want {
		t.Fatalf("wallet %q transaction count = %d, want %d", transactionType, count, want)
	}
}
