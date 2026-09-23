package membership

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

func TestMembershipEndpointsRequireAuthentication(t *testing.T) {
	_, server := membershipHTTPEnvironment(t, NewCatalog(membershipPlans))

	requests := []struct {
		method  string
		path    string
		payload any
	}{
		{method: http.MethodGet, path: "/api/membership"},
		{method: http.MethodPost, path: "/api/membership/buy", payload: map[string]any{"plan_slug": "standard"}},
		{method: http.MethodPost, path: "/api/membership/cancel", payload: map[string]any{"history_id": "id"}},
	}
	for _, request := range requests {
		response := requestMembershipJSON(t, server.Client(), request.method, server.URL+request.path, request.payload)
		if response.status != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", request.method, request.path, response.status)
		}
	}
}

func TestMembershipLifecycleManualFlowAndFrontendPayloads(t *testing.T) {
	db, server := membershipHTTPEnvironment(t, NewCatalog(membershipPlans))
	ownerClient := membershipClient(t, server)
	registerMembershipUser(t, ownerClient, server.URL, "member@example.com")

	initial := requestMembershipJSON(t, ownerClient, http.MethodGet, server.URL+"/api/membership", nil)
	if initial.status != http.StatusOK || initial.body["wallet_balance"] != float64(0) {
		t.Fatalf("initial membership = status %d body %v", initial.status, initial.body)
	}
	if plans := initial.body["plans"].([]any); len(plans) != len(membershipPlans) {
		t.Fatalf("plan count = %d, want %d", len(plans), len(membershipPlans))
	}
	if history := initial.body["history"].([]any); len(history) != 0 {
		t.Fatalf("initial history = %v, want empty", history)
	}

	noFunds := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "standard"})
	if noFunds.status != http.StatusPaymentRequired {
		t.Fatalf("insufficient-funds status = %d body %v, want 402", noFunds.status, noFunds.body)
	}
	deposit := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/wallet/deposit", map[string]any{"amount": 100000})
	if deposit.status != http.StatusOK || deposit.body["new_balance"] != float64(100000) {
		t.Fatalf("deposit = status %d body %v", deposit.status, deposit.body)
	}

	malformed := requestRawMembership(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", []byte(`{"plan_slug":`))
	if malformed.status != http.StatusBadRequest {
		t.Fatalf("malformed purchase status = %d, want 400", malformed.status)
	}
	missingSlug := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{})
	if missingSlug.status != http.StatusBadRequest {
		t.Fatalf("missing-slug status = %d, want 400", missingSlug.status)
	}
	unknown := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "unknown"})
	if unknown.status != http.StatusNotFound {
		t.Fatalf("unknown-plan status = %d, want 404", unknown.status)
	}
	invalid := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "invalid"})
	if invalid.status != http.StatusBadRequest {
		t.Fatalf("invalid-plan status = %d, want 400", invalid.status)
	}

	firstPurchase := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "standard"})
	if firstPurchase.status != http.StatusCreated || firstPurchase.body["new_balance"] != float64(70000) {
		t.Fatalf("first purchase = status %d body %v", firstPurchase.status, firstPurchase.body)
	}
	firstMembership := firstPurchase.body["membership"].(map[string]any)
	if firstMembership["slug"] != "standard" || firstMembership["expires_at"] != "2030-02-01" || firstMembership["active"] != true {
		t.Fatalf("first membership payload = %v", firstMembership)
	}
	firstHistory := firstPurchase.body["history_item"].(map[string]any)
	firstHistoryID := firstHistory["id"].(string)
	if firstHistory["plan_slug"] != "standard" || firstHistory["amount"] != float64(30000) || firstHistory["status"] != StatusActive {
		t.Fatalf("first history payload = %v", firstHistory)
	}

	owner, err := user.NewStore(db).FindByEmail(t.Context(), "member@example.com")
	if err != nil {
		t.Fatalf("load owner: %v", err)
	}
	otherClient := membershipClient(t, server)
	registerMembershipUser(t, otherClient, server.URL, "other@example.com")
	crossUser := requestMembershipJSON(t, otherClient, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": firstHistoryID})
	if crossUser.status != http.StatusNotFound {
		t.Fatalf("cross-user cancellation = status %d body %v, want 404", crossUser.status, crossUser.body)
	}
	notFound := requestMembershipJSON(t, otherClient, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": "missing"})
	if notFound.status != http.StatusNotFound || notFound.body["message"] != crossUser.body["message"] {
		t.Fatalf("foreign and missing responses differ: foreign=%v missing=%v", crossUser, notFound)
	}
	if got := loadHistory(t, db, firstHistoryID).Status; got != StatusActive {
		t.Fatalf("history after cross-user cancellation = %q, want active", got)
	}
	if loadMembershipUser(t, db, owner.ID).WalletBalance != 70000 {
		t.Fatal("owner wallet changed after cross-user cancellation")
	}

	secondPurchase := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "standard"})
	if secondPurchase.status != http.StatusCreated || secondPurchase.body["new_balance"] != float64(40000) {
		t.Fatalf("extension = status %d body %v", secondPurchase.status, secondPurchase.body)
	}
	secondMembership := secondPurchase.body["membership"].(map[string]any)
	if secondMembership["expires_at"] != "2030-03-03" {
		t.Fatalf("extended expiry = %v, want 2030-03-03", secondMembership["expires_at"])
	}
	secondHistoryID := secondPurchase.body["history_item"].(map[string]any)["id"].(string)

	loaded := requestMembershipJSON(t, ownerClient, http.MethodGet, server.URL+"/api/membership", nil)
	if loaded.status != http.StatusOK || len(loaded.body["history"].([]any)) != 2 {
		t.Fatalf("loaded membership = status %d body %v", loaded.status, loaded.body)
	}

	cancelled := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": secondHistoryID})
	if cancelled.status != http.StatusOK || cancelled.body["new_balance"] != float64(70000) {
		t.Fatalf("cancellation = status %d body %v", cancelled.status, cancelled.body)
	}
	storedAfterCancel := loadMembershipUser(t, db, owner.ID)
	if storedAfterCancel.MembershipSlug != nil || storedAfterCancel.MembershipExpiresAt != nil {
		t.Fatalf("membership after cancellation = %+v, want cleared", storedAfterCancel)
	}
	refund := onlyTransactionOfType(t, db, owner.ID, wallet.TransactionTypeRefund)
	if refund.Amount != 30000 {
		t.Fatalf("refund amount = %d, want 30000", refund.Amount)
	}

	repeated := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": secondHistoryID})
	if repeated.status != http.StatusConflict {
		t.Fatalf("repeated cancellation = status %d body %v, want 409", repeated.status, repeated.body)
	}
	firstCancellation := requestMembershipJSON(t, ownerClient, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": firstHistoryID})
	if firstCancellation.status != http.StatusOK || firstCancellation.body["new_balance"] != float64(100000) {
		t.Fatalf("first-history cancellation = status %d body %v", firstCancellation.status, firstCancellation.body)
	}

	finalState := requestMembershipJSON(t, ownerClient, http.MethodGet, server.URL+"/api/membership", nil)
	if finalState.status != http.StatusOK || finalState.body["wallet_balance"] != float64(100000) {
		t.Fatalf("final membership state = status %d body %v", finalState.status, finalState.body)
	}
	current := finalState.body["membership"].(map[string]any)
	if current["active"] != false || current["slug"] != nil || current["expires_at"] != nil {
		t.Fatalf("final current membership = %v, want inactive and cleared", current)
	}
	history := finalState.body["history"].([]any)
	if len(history) != 2 || history[0].(map[string]any)["status"] != StatusCancelled || history[1].(map[string]any)["status"] != StatusCancelled {
		t.Fatalf("final history = %v, want two cancelled rows", history)
	}
}

func TestMembershipCancellationOutsidePurchaseDayReturnsConflict(t *testing.T) {
	db, server := membershipHTTPEnvironment(t, NewCatalog(membershipPlans))
	client := membershipClient(t, server)
	registerMembershipUser(t, client, server.URL, "past-http@example.com")
	member, err := user.NewStore(db).FindByEmail(t.Context(), "past-http@example.com")
	if err != nil {
		t.Fatalf("load member: %v", err)
	}
	item := createHistory(t, db, member.ID, "past-http", "standard", membershipNow.AddDate(0, 0, -1), dateAt(2030, time.February, 1), 30000, StatusActive)

	response := requestMembershipJSON(t, client, http.MethodPost, server.URL+"/api/membership/cancel", map[string]any{"history_id": item.ID})
	if response.status != http.StatusConflict {
		t.Fatalf("past cancellation = status %d body %v, want 409", response.status, response.body)
	}
	if loadHistory(t, db, item.ID).Status != StatusActive {
		t.Fatal("past history changed after rejected HTTP cancellation")
	}
}

func TestUnavailablePlanFileReturnsSafeServerError(t *testing.T) {
	_, server := membershipHTTPEnvironment(t, LoadPlans(filepath.Join(t.TempDir(), "missing.json")))
	client := membershipClient(t, server)
	registerMembershipUser(t, client, server.URL, "config@example.com")

	get := requestMembershipJSON(t, client, http.MethodGet, server.URL+"/api/membership", nil)
	if get.status != http.StatusInternalServerError || get.body["status"] != "error" {
		t.Fatalf("GET with missing plans = status %d body %v, want safe 500", get.status, get.body)
	}
	buy := requestMembershipJSON(t, client, http.MethodPost, server.URL+"/api/membership/buy", map[string]any{"plan_slug": "standard"})
	if buy.status != http.StatusInternalServerError || buy.body["status"] != "error" {
		t.Fatalf("buy with missing plans = status %d body %v, want safe 500", buy.status, buy.body)
	}
}

func membershipHTTPEnvironment(t *testing.T, catalog Catalog) (*gorm.DB, *httptest.Server) {
	t.Helper()
	db, _ := membershipTestEnvironment(t)
	authHandler, err := auth.NewHandler(user.NewStore(db), auth.NewSessionStore(time.Hour), false)
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}
	walletService := wallet.NewService(db)
	service := NewService(db, walletService, catalog)
	service.now = func() time.Time { return membershipNow }
	service.location = time.Local
	server := httptest.NewServer(httpapi.NewRouter(
		authHandler,
		wallet.NewHandler(walletService, authHandler),
		NewHandler(service, authHandler),
	))
	t.Cleanup(server.Close)
	return db, server
}

type membershipEndpointResponse struct {
	status int
	body   map[string]any
}

func membershipClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := *server.Client()
	client.Jar = jar
	return &client
}

func registerMembershipUser(t *testing.T, client *http.Client, serverURL string, email string) {
	t.Helper()
	response := requestMembershipJSON(t, client, http.MethodPost, serverURL+"/api/auth/register", map[string]any{
		"email": email, "password": "correct horse battery staple", "password2": "correct horse battery staple",
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d body %v, want 201", response.status, response.body)
	}
}

func requestMembershipJSON(t *testing.T, client *http.Client, method string, url string, payload any) membershipEndpointResponse {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	return sendMembershipRequest(t, client, method, url, body)
}

func requestRawMembership(t *testing.T, client *http.Client, method string, url string, payload []byte) membershipEndpointResponse {
	t.Helper()
	return sendMembershipRequest(t, client, method, url, bytes.NewReader(payload))
}

func sendMembershipRequest(t *testing.T, client *http.Client, method string, url string, body io.Reader) membershipEndpointResponse {
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
	return membershipEndpointResponse{status: response.StatusCode, body: payload}
}
