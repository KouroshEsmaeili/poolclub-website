package booking

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
)

func TestBookingCreateEndpointMatchesFrontendPayload(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	walletService := wallet.NewService(db)
	walletHandler := wallet.NewHandler(walletService, authHandler)
	bookingHandler := NewHandler(service, authHandler)
	server := httptest.NewServer(httpapi.NewRouter(authHandler, walletHandler, bookingHandler))
	t.Cleanup(server.Close)

	unauthenticated := requestBookingJSON(t, server.Client(), http.MethodPost, server.URL+"/api/bookings/create", map[string]any{
		"date": "2030-01-03", "time": "10:00", "duration": 60, "type": TypeFreeSwim,
	})
	if unauthenticated.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want %d", unauthenticated.status, http.StatusUnauthorized)
	}

	client := clientWithJar(t, server)
	registerBookingUser(t, client, server.URL, "frontend@example.com")
	deposit := requestBookingJSON(t, client, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 200000})
	if deposit.status != http.StatusOK {
		t.Fatalf("deposit status = %d, want 200; body = %v", deposit.status, deposit.body)
	}

	created := requestBookingJSON(t, client, http.MethodPost, server.URL+"/api/bookings/create", map[string]any{
		"date":     "2030-01-03",
		"time":     "10:00",
		"duration": 60,
		"type":     TypeLaneTrainingRequest,
	})
	if created.status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body = %v", created.status, created.body)
	}
	if created.body["status"] != "success" || created.body["lane"] != float64(1) || created.body["new_balance"] != float64(120000) {
		t.Fatalf("create response = %v, want success, lane 1, balance 120000", created.body)
	}

	bookingID := int64(created.body["booking_id"].(float64))
	stored := loadBooking(t, db, bookingID)
	if stored.Type != TypeLaneTraining || stored.Lane == nil || *stored.Lane != 1 {
		t.Fatalf("stored booking = %+v, want normalized lane training on lane 1", stored)
	}
	transaction := onlyPurchase(t, db, stored.UserID)
	if transaction.Amount != -DefaultLaneTrainingPrice {
		t.Fatalf("purchase amount = %d, want -%d", transaction.Amount, DefaultLaneTrainingPrice)
	}
}

func TestBookingCreateEndpointErrorsMatchFlaskContract(t *testing.T) {
	_, service := bookingTestEnvironment(t)
	db := service.db
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	server := httptest.NewServer(httpapi.NewRouter(authHandler, NewHandler(service, authHandler)))
	t.Cleanup(server.Close)
	client := clientWithJar(t, server)
	registerBookingUser(t, client, server.URL, "errors@example.com")

	tests := []struct {
		name   string
		body   map[string]any
		status int
	}{
		{name: "missing", body: map[string]any{"date": "2030-01-03"}, status: http.StatusBadRequest},
		{name: "invalid duration", body: map[string]any{"date": "2030-01-03", "time": "10:00", "duration": 0, "type": TypeFreeSwim}, status: http.StatusBadRequest},
		{name: "invalid date", body: map[string]any{"date": "bad", "time": "10:00", "duration": 60, "type": TypeFreeSwim}, status: http.StatusBadRequest},
		{name: "insufficient", body: map[string]any{"date": "2030-01-03", "time": "10:00", "duration": 60, "type": TypeFreeSwim}, status: http.StatusPaymentRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := requestBookingJSON(t, client, http.MethodPost, server.URL+"/api/bookings/create", test.body)
			if response.status != test.status || response.body["status"] != "error" {
				t.Fatalf("response = status %d body %v, want status %d error contract", response.status, response.body, test.status)
			}
		})
	}
}

func TestCancellationEnforcesOwnershipWithoutChangingOtherPolicies(t *testing.T) {
	db, service := bookingTestEnvironment(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create authentication handler: %v", err)
	}
	server := httptest.NewServer(httpapi.NewRouter(authHandler, NewHandler(service, authHandler)))
	t.Cleanup(server.Close)

	ownerClient := clientWithJar(t, server)
	registerBookingUser(t, ownerClient, server.URL, "owner@example.com")
	owner, err := user.NewStore(db).FindByEmail(t.Context(), "owner@example.com")
	if err != nil {
		t.Fatalf("load owner: %v", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", owner.ID).UpdateColumn("wallet_balance", 50000).Error; err != nil {
		t.Fatalf("seed owner wallet: %v", err)
	}
	owned := insertBooking(t, db, owner.ID, futureInput(TypeFreeSwim, "10:00", 60), nil, StatusActive)
	target := insertBooking(t, db, owner.ID, futureInput(TypeFreeSwim, "12:00", 60), nil, StatusActive)
	past := insertBooking(t, db, owner.ID, CreateInput{
		Date: "2030-01-01", Time: "10:00", Duration: 60, Type: TypeFreeSwim,
	}, nil, StatusExpired)

	unauthenticated := requestBookingJSON(t, server.Client(), http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": owned.ID})
	if unauthenticated.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated cancellation status = %d, want 401", unauthenticated.status)
	}

	missingID := requestBookingJSON(t, ownerClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{})
	if missingID.status != http.StatusBadRequest {
		t.Fatalf("missing-id status = %d, want 400", missingID.status)
	}
	notFound := requestBookingJSON(t, ownerClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": 999999})
	if notFound.status != http.StatusNotFound {
		t.Fatalf("missing-booking status = %d, want 404", notFound.status)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		cancelled := requestBookingJSON(t, ownerClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": fmtInt(owned.ID)})
		if cancelled.status != http.StatusOK || cancelled.body["status"] != "success" {
			t.Fatalf("cancel attempt %d = status %d body %v, want success", attempt, cancelled.status, cancelled.body)
		}
	}
	if got := loadBooking(t, db, owned.ID).Status; got != StatusCancelled {
		t.Fatalf("booking status = %q, want cancelled", got)
	}
	pastCancellation := requestBookingJSON(t, ownerClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": past.ID})
	if pastCancellation.status != http.StatusOK || loadBooking(t, db, past.ID).Status != StatusCancelled {
		t.Fatalf("past cancellation = status %d body %v, want success", pastCancellation.status, pastCancellation.body)
	}

	otherClient := clientWithJar(t, server)
	registerBookingUser(t, otherClient, server.URL, "other-user@example.com")
	other, err := user.NewStore(db).FindByEmail(t.Context(), "other-user@example.com")
	if err != nil {
		t.Fatalf("load other user: %v", err)
	}
	crossUser := requestBookingJSON(t, otherClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": target.ID})
	if crossUser.status != http.StatusNotFound || crossUser.body["status"] != "error" {
		t.Fatalf("cross-user cancellation = status %d body %v, want 404 error", crossUser.status, crossUser.body)
	}
	if got := loadBooking(t, db, target.ID).Status; got != StatusActive {
		t.Fatalf("target status after cross-user attempt = %q, want active", got)
	}
	otherMissing := requestBookingJSON(t, otherClient, http.MethodPost, server.URL+"/api/bookings/cancel", map[string]any{"booking_id": 999999})
	if otherMissing.status != http.StatusNotFound || otherMissing.body["message"] != crossUser.body["message"] {
		t.Fatalf("foreign and missing responses differ: foreign=%v missing=%v", crossUser, otherMissing)
	}

	if got := loadBalance(t, db, owner.ID); got != 50000 {
		t.Fatalf("owner balance after cancellation = %d, want 50000", got)
	}
	assertWalletTransactionCount(t, db, owner.ID, 0)
	if got := loadBalance(t, db, other.ID); got != 0 {
		t.Fatalf("other-user balance after cancellation attempt = %d, want 0", got)
	}
	assertWalletTransactionCount(t, db, other.ID, 0)
}

type bookingEndpointResponse struct {
	status int
	body   map[string]any
}

func clientWithJar(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := *server.Client()
	client.Jar = jar
	return &client
}

func registerBookingUser(t *testing.T, client *http.Client, serverURL string, email string) {
	t.Helper()
	response := requestBookingJSON(t, client, http.MethodPost, serverURL+"/api/auth/register", map[string]any{
		"email": email, "password": "correct horse battery staple", "password2": "correct horse battery staple",
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d, want 201; body = %v", response.status, response.body)
	}
}

func requestBookingJSON(t *testing.T, client *http.Client, method string, url string, payload any) bookingEndpointResponse {
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
	return bookingEndpointResponse{status: response.StatusCode, body: body}
}

func fmtInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
