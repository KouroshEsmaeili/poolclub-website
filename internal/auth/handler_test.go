package auth_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"gorm.io/gorm"
)

const werkzeugHash = "scrypt:32768:8:1$fixedsalt123456$37f7f2259be95268d2505bce8f233963bdb32597f392d9cd5ebb20637914bf5e700e8ae6c1a6fdf03a06df39a5231108e055ccdb90d13198103667a71269a419"

type testEnvironment struct {
	db     *gorm.DB
	server *httptest.Server
	client *http.Client
}

type apiResponse struct {
	status  int
	body    map[string]any
	cookies []*http.Cookie
}

func TestRegistrationAndAuthenticatedMe(t *testing.T) {
	environment := newTestEnvironment(t, false)

	response := environment.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"first_name": "  Ada  ",
		"last_name":  "  Lovelace ",
		"email":      "  ADA@Example.COM ",
		"password":   "correct horse battery staple",
		"password2":  "correct horse battery staple",
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %v", response.status, http.StatusCreated, response.body)
	}

	responseUser := response.body["user"].(map[string]any)
	if responseUser["email"] != "ada@example.com" {
		t.Fatalf("registered email = %v, want ada@example.com", responseUser["email"])
	}
	if responseUser["first_name"] != "Ada" || responseUser["last_name"] != "Lovelace" {
		t.Fatalf("registered names = %v %v, want Ada Lovelace", responseUser["first_name"], responseUser["last_name"])
	}
	if _, exists := responseUser["password_hash"]; exists {
		t.Fatal("registration response exposed password_hash")
	}

	stored := loadUser(t, environment.db, "ada@example.com")
	if stored.PasswordHash == "correct horse battery staple" {
		t.Fatal("registration stored plaintext password")
	}
	if valid, _ := auth.VerifyPassword(stored.PasswordHash, "correct horse battery staple"); !valid {
		t.Fatal("stored password hash did not verify")
	}

	assertSessionCookie(t, response.cookies, false)

	me := environment.request(t, http.MethodGet, "/api/auth/me", nil)
	if me.status != http.StatusOK {
		t.Fatalf("authenticated me status = %d, want %d; body = %v", me.status, http.StatusOK, me.body)
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	environment := newTestEnvironment(t, false)
	register(t, environment, "member@example.com", "password")

	response := environment.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email":     " MEMBER@EXAMPLE.COM ",
		"password":  "password",
		"password2": "password",
	})
	if response.status != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d; body = %v", response.status, http.StatusConflict, response.body)
	}
}

func TestLoginFailuresAndSuccess(t *testing.T) {
	environment := newTestEnvironment(t, false)
	register(t, environment, "member@example.com", "correct password")
	environment.request(t, http.MethodPost, "/api/auth/logout", nil)

	wrongPassword := environment.request(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "member@example.com",
		"password": "wrong password",
	})
	if wrongPassword.status != http.StatusUnauthorized {
		t.Fatalf("wrong-password status = %d, want %d", wrongPassword.status, http.StatusUnauthorized)
	}

	missingUser := environment.request(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "missing@example.com",
		"password": "correct password",
	})
	if missingUser.status != http.StatusUnauthorized {
		t.Fatalf("missing-user status = %d, want %d", missingUser.status, http.StatusUnauthorized)
	}
	if wrongPassword.body["error"] != missingUser.body["error"] {
		t.Fatalf("credential errors differ: %v and %v", wrongPassword.body, missingUser.body)
	}

	valid := environment.request(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    " MEMBER@EXAMPLE.COM ",
		"password": "correct password",
	})
	if valid.status != http.StatusOK {
		t.Fatalf("valid-login status = %d, want %d; body = %v", valid.status, http.StatusOK, valid.body)
	}
	assertSessionCookie(t, valid.cookies, false)
}

func TestUnauthenticatedMe(t *testing.T) {
	environment := newTestEnvironment(t, false)

	response := environment.request(t, http.MethodGet, "/api/auth/me", nil)
	if response.status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated me status = %d, want %d", response.status, http.StatusUnauthorized)
	}
}

func TestLogoutInvalidatesSession(t *testing.T) {
	environment := newTestEnvironment(t, false)
	registered := register(t, environment, "member@example.com", "password")
	sessionCookie := findSessionCookie(t, registered.cookies)

	logout := environment.request(t, http.MethodPost, "/api/auth/logout", nil)
	if logout.status != http.StatusOK {
		t.Fatalf("logout status = %d, want %d", logout.status, http.StatusOK)
	}

	request, err := http.NewRequest(http.MethodGet, environment.server.URL+"/api/auth/me", nil)
	if err != nil {
		t.Fatalf("create stale-session request: %v", err)
	}
	request.AddCookie(sessionCookie)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("send stale-session request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("stale session status = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}
}

func TestSecureSessionCookieAttributes(t *testing.T) {
	environment := newTestEnvironment(t, true)

	response := register(t, environment, "member@example.com", "password")
	assertSessionCookie(t, response.cookies, true)
}

func TestWerkzeugHashRehashedOnLogin(t *testing.T) {
	environment := newTestEnvironment(t, false)
	firstName := "Legacy"
	legacyUser := model.User{
		Email:        "legacy@example.com",
		PasswordHash: werkzeugHash,
		FirstName:    &firstName,
	}
	if err := environment.db.Create(&legacyUser).Error; err != nil {
		t.Fatalf("create legacy user: %v", err)
	}

	response := environment.request(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "legacy@example.com",
		"password": "correct horse battery staple",
	})
	if response.status != http.StatusOK {
		t.Fatalf("legacy login status = %d, want %d; body = %v", response.status, http.StatusOK, response.body)
	}

	rehashed := loadUser(t, environment.db, "legacy@example.com")
	if !strings.HasPrefix(rehashed.PasswordHash, "$2") {
		t.Fatalf("legacy hash was not replaced with bcrypt: %q", rehashed.PasswordHash)
	}
}

func newTestEnvironment(t *testing.T, secureCookie bool) *testEnvironment {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("open temporary database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get temporary database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close temporary database: %v", err)
		}
	})

	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("auto-migrate temporary users table: %v", err)
	}

	handler, err := auth.NewHandler(
		user.NewStore(db),
		auth.NewSessionStore(time.Hour),
		secureCookie,
	)
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}

	server := httptest.NewServer(httpapi.NewRouter(handler))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar

	return &testEnvironment{db: db, server: server, client: client}
}

func (e *testEnvironment) request(t *testing.T, method string, path string, payload any) apiResponse {
	t.Helper()

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequest(method, e.server.URL+path, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := e.client.Do(request)
	if err != nil {
		t.Fatalf("send request: %v", err)
	}
	defer response.Body.Close()

	decoded := make(map[string]any)
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	return apiResponse{
		status:  response.StatusCode,
		body:    decoded,
		cookies: response.Cookies(),
	}
}

func register(t *testing.T, environment *testEnvironment, email string, password string) apiResponse {
	t.Helper()
	response := environment.request(t, http.MethodPost, "/api/auth/register", map[string]string{
		"email":     email,
		"password":  password,
		"password2": password,
	})
	if response.status != http.StatusCreated {
		t.Fatalf("register status = %d, want %d; body = %v", response.status, http.StatusCreated, response.body)
	}
	return response
}

func loadUser(t *testing.T, db *gorm.DB, email string) model.User {
	t.Helper()
	var stored model.User
	if err := db.Where("email = ?", email).First(&stored).Error; err != nil {
		t.Fatalf("load stored user: %v", err)
	}
	return stored
}

func assertSessionCookie(t *testing.T, cookies []*http.Cookie, secure bool) {
	t.Helper()
	cookie := findSessionCookie(t, cookies)
	if !cookie.HttpOnly {
		t.Error("session cookie HttpOnly = false, want true")
	}
	if cookie.Secure != secure {
		t.Errorf("session cookie Secure = %t, want %t", cookie.Secure, secure)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Expires.Before(time.Now()) {
		t.Errorf("session cookie expiration = %v, want future time", cookie.Expires)
	}
	if len(cookie.Value) < 40 {
		t.Errorf("session identifier length = %d, want at least 40", len(cookie.Value))
	}
}

func findSessionCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == "poolclub_session" {
			return cookie
		}
	}
	t.Fatal("session cookie is missing")
	return nil
}
