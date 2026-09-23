package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/booking"
	"github.com/KouroshEsmaeili/poolclub-website/internal/classes"
	"github.com/KouroshEsmaeili/poolclub-website/internal/database"
	"github.com/KouroshEsmaeili/poolclub-website/internal/events"
	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
	"github.com/KouroshEsmaeili/poolclub-website/internal/info"
	"github.com/KouroshEsmaeili/poolclub-website/internal/membership"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"github.com/KouroshEsmaeili/poolclub-website/migrations"
)

type fixedRankings struct {
	result info.Rankings
	err    error
}

func (f fixedRankings) Fetch(context.Context) (info.Rankings, error) { return f.result, f.err }

type webEnvironment struct {
	handler http.Handler
	users   *user.Store
	auth    *auth.Handler
}

func TestPublicAndAuthenticationPages(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{result: sampleRankings()})

	for _, route := range []string{"/", "/auth/login", "/auth/register"} {
		response := performRequest(environment.handler, http.MethodGet, route, "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body=%s", route, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "{%") || strings.Contains(response.Body.String(), "{{") {
			t.Fatalf("GET %s leaked a template artifact", route)
		}
	}

	home := performRequest(environment.handler, http.MethodGet, "/", "", nil).Body.String()
	for _, expected := range []string{"/auth/login", "/static/css/styles.min.css", "/static/js/app.js", `id="live-rankings"`, "100m Free", "49.10"} {
		if !strings.Contains(home, expected) {
			t.Errorf("home does not contain %q", expected)
		}
	}
}

func TestBrowserRegistrationDashboardLogoutFlow(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{result: sampleRankings()})
	unauthenticated := performRequest(environment.handler, http.MethodGet, "/dashboard", "", nil)
	if unauthenticated.Code != http.StatusFound || unauthenticated.Header().Get("Location") != "/auth/login?next=%2Fdashboard" {
		t.Fatalf("unauthenticated dashboard = %d %q", unauthenticated.Code, unauthenticated.Header().Get("Location"))
	}

	form := url.Values{
		"first_name": {"<script>alert(1)</script>"}, "last_name": {"User"},
		"email": {"person@example.com"}, "password": {"correct horse"}, "password2": {"correct horse"},
	}.Encode()
	registered := performRequest(environment.handler, http.MethodPost, "/auth/register", form, nil)
	if registered.Code != http.StatusFound || registered.Header().Get("Location") != "/dashboard" {
		t.Fatalf("register = %d %q, body=%s", registered.Code, registered.Header().Get("Location"), registered.Body.String())
	}
	sessionCookie := responseCookie(t, registered, "poolclub_session")

	dashboard := performRequest(environment.handler, http.MethodGet, "/dashboard", "", sessionCookie)
	if dashboard.Code != http.StatusOK {
		t.Fatalf("authenticated dashboard = %d; body=%s", dashboard.Code, dashboard.Body.String())
	}
	if strings.Contains(dashboard.Body.String(), "<script>alert(1)</script>") || !strings.Contains(dashboard.Body.String(), "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatal("dashboard did not HTML-escape the user's name")
	}

	loggedOut := performRequest(environment.handler, http.MethodGet, "/auth/logout", "", sessionCookie)
	if loggedOut.Code != http.StatusFound || loggedOut.Header().Get("Location") != "/" {
		t.Fatalf("logout = %d %q", loggedOut.Code, loggedOut.Header().Get("Location"))
	}
	afterLogout := performRequest(environment.handler, http.MethodGet, "/dashboard", "", sessionCookie)
	if afterLogout.Code != http.StatusFound {
		t.Fatalf("dashboard after logout = %d, want 302", afterLogout.Code)
	}
}

func TestStaticAssetsAndFrontendContracts(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{result: sampleRankings()})
	css := performRequest(environment.handler, http.MethodGet, "/static/css/styles.min.css", "", nil)
	if css.Code != http.StatusOK || css.Body.Len() == 0 {
		t.Fatalf("CSS response = %d with %d bytes", css.Code, css.Body.Len())
	}
	javascript := performRequest(environment.handler, http.MethodGet, "/static/js/app.js", "", nil)
	if javascript.Code != http.StatusOK {
		t.Fatalf("JavaScript response = %d", javascript.Code)
	}
	body := javascript.Body.String()
	for _, expected := range []string{"/api/bookings/create", "/api/classes/enroll", "/api/events/register", "/api/live-rankings", "item.event", "item.time"} {
		if !strings.Contains(body, expected) {
			t.Errorf("JavaScript does not reference %q", expected)
		}
	}
	for _, stale := range []string{"item.age_group", "item.stroke"} {
		if strings.Contains(body, stale) {
			t.Errorf("JavaScript still references stale field %q", stale)
		}
	}
}

func TestRankingsFailureIsOptional(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{err: errors.New("upstream unavailable")})
	response := performRequest(environment.handler, http.MethodGet, "/", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("home with unavailable rankings = %d; body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "امکان دریافت اطلاعات زنده وجود ندارد") {
		t.Fatal("home did not show the rankings fallback")
	}
}

func TestAuthenticatedPageRoutesRender(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{result: sampleRankings()})
	created, err := environment.users.Create(context.Background(), "pages@example.com", mustHash(t, "secret123"), "Page", "User")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	login := url.Values{"email": {created.Email}, "password": {"secret123"}}.Encode()
	loggedIn := performRequest(environment.handler, http.MethodPost, "/auth/login", login, nil)
	cookie := responseCookie(t, loggedIn, "poolclub_session")
	for _, route := range []string{"/dashboard", "/dashboard/wallet", "/dashboard/membership", "/dashboard/bookings", "/dashboard/classes", "/dashboard/events", "/dashboard/profile"} {
		response := performRequest(environment.handler, http.MethodGet, route, "", cookie)
		if response.Code != http.StatusOK {
			t.Errorf("GET %s = %d; body=%s", route, response.Code, response.Body.String())
		}
	}
}

func TestRepositoryDemoDataFreshCloneSetup(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	dataDir := filepath.Join(repositoryRoot, "data")
	db, err := database.Open(filepath.Join(t.TempDir(), "fresh-clone.db"))
	if err != nil {
		t.Fatalf("open fresh-clone database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get fresh-clone database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close fresh-clone database: %v", err)
		}
	})
	if _, err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply tracked migrations: %v", err)
	}

	prices := booking.LoadPrices(filepath.Join(dataDir, "prices.json"))
	if prices.FreeSwim <= 0 || prices.LaneTraining <= 0 {
		t.Fatalf("repository booking prices are not usable: %+v", prices)
	}
	plans := membership.LoadPlans(filepath.Join(dataDir, "memberships.json"))
	if configured, err := plans.Plans(); err != nil || len(configured) == 0 {
		t.Fatalf("repository membership plans: count=%d err=%v", len(configured), err)
	}
	classCatalog := classes.LoadCatalog(filepath.Join(dataDir, "classes.json"))
	if configured, err := classCatalog.Classes(); err != nil || len(configured) == 0 {
		t.Fatalf("repository class catalog: count=%d err=%v", len(configured), err)
	}
	eventCatalog := events.LoadCatalog(filepath.Join(dataDir, "events.json"))
	if configured, err := eventCatalog.Events(); err != nil || len(configured) == 0 {
		t.Fatalf("repository event catalog: count=%d err=%v", len(configured), err)
	}

	userStore := user.NewStore(db)
	sessions := auth.NewSessionStore(time.Hour)
	authHandler, err := auth.NewHandler(userStore, sessions, false)
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}
	walletService := wallet.NewService(db)
	bookingService := booking.NewService(db, walletService, prices)
	membershipService := membership.NewService(db, walletService, plans)
	classService := classes.NewService(db, walletService, classCatalog)
	eventService := events.NewService(db, walletService, eventCatalog)
	bookingHandler := booking.NewHandler(bookingService, authHandler)
	membershipHandler := membership.NewHandler(membershipService, authHandler)
	classHandler := classes.NewHandler(classService, authHandler)
	eventHandler := events.NewHandler(eventService, authHandler)
	walletHandler := wallet.NewHandler(walletService, authHandler)
	infoHandler := info.NewHandler(
		filepath.Join(dataDir, "pools.json"),
		filepath.Join(dataDir, "programmes.json"),
		fixedRankings{result: sampleRankings()},
	)
	webHandler, err := NewHandler(Dependencies{
		Auth: authHandler, Users: userStore, Wallet: walletService, Bookings: bookingService,
		Memberships: membershipService, Classes: classService, Events: eventService,
		Rankings: fixedRankings{result: sampleRankings()}, DataDir: dataDir,
		StaticDir: filepath.Join(repositoryRoot, "app", "static"),
	})
	if err != nil {
		t.Fatalf("create web handler: %v", err)
	}
	handler := httpapi.NewRouter(authHandler, walletHandler, bookingHandler, membershipHandler, classHandler, eventHandler, infoHandler, webHandler)

	for _, route := range []string{"/", "/auth/register", "/auth/login", "/api/pools", "/api/programmes"} {
		response := performRequest(handler, http.MethodGet, route, "", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d; body=%s", route, response.Code, response.Body.String())
		}
	}
	home := performRequest(handler, http.MethodGet, "/", "", nil).Body.String()
	if !strings.Contains(home, "باشگاه شنای موج آبی") || !strings.Contains(home, "demo-main-pool") {
		t.Fatal("home did not render repository demo data")
	}
	assertLocalStaticReferencesExist(t, repositoryRoot, home)

	form := url.Values{
		"first_name": {"Demo"}, "last_name": {"User"}, "email": {"fresh@example.invalid"},
		"password": {"demo-secret"}, "password2": {"demo-secret"},
	}.Encode()
	registered := performRequest(handler, http.MethodPost, "/auth/register", form, nil)
	if registered.Code != http.StatusFound {
		t.Fatalf("fresh-clone registration = %d; body=%s", registered.Code, registered.Body.String())
	}
	cookie := responseCookie(t, registered, "poolclub_session")
	for _, route := range []string{"/dashboard/membership", "/dashboard/classes", "/dashboard/events"} {
		response := performRequest(handler, http.MethodGet, route, "", cookie)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s = %d; body=%s", route, response.Code, response.Body.String())
		}
	}
}

func TestPasswordOnlyProfileFormAndSafeLoginRedirect(t *testing.T) {
	environment := newWebEnvironment(t, fixedRankings{result: sampleRankings()})
	created, err := environment.users.Create(context.Background(), "profile@example.com", mustHash(t, "old-secret"), "Profile", "User")
	if err != nil {
		t.Fatalf("create profile user: %v", err)
	}
	login := url.Values{"email": {created.Email}, "password": {"old-secret"}, "next": {"https://example.net/stolen"}}.Encode()
	loggedIn := performRequest(environment.handler, http.MethodPost, "/auth/login", login, nil)
	if loggedIn.Header().Get("Location") != "/dashboard" {
		t.Fatalf("external next redirect = %q, want /dashboard", loggedIn.Header().Get("Location"))
	}
	cookie := responseCookie(t, loggedIn, "poolclub_session")

	passwordForm := url.Values{
		"profile_action": {"password"}, "current_password": {"old-secret"},
		"new_password": {"new-secret"}, "new_password2": {"new-secret"},
	}.Encode()
	updated := performRequest(environment.handler, http.MethodPost, "/dashboard/profile", passwordForm, cookie)
	if updated.Code != http.StatusFound || updated.Header().Get("Location") != "/dashboard/profile" {
		t.Fatalf("password-only profile update = %d %q; body=%s", updated.Code, updated.Header().Get("Location"), updated.Body.String())
	}
	if _, err := environment.auth.AuthenticateCredentials(httptest.NewRequest(http.MethodPost, "/", nil), created.Email, "new-secret"); err != nil {
		t.Fatalf("authenticate with updated password: %v", err)
	}
}

func newWebEnvironment(t *testing.T, rankings info.RankingsFetcher) webEnvironment {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "web.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get database connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	if _, err := migrations.Apply(context.Background(), db); err != nil {
		t.Fatalf("apply tracked migrations: %v", err)
	}

	dataDir := t.TempDir()
	writeFixture(t, dataDir, "site.json", `{"brand":"باشگاه آزمون","tagline":"شنا برای همه","city":"تهران"}`)
	writeFixture(t, dataDir, "hours.json", `{"timezone":"Asia/Tehran","weekly":[{"dow":"شنبه","open":"08:00","close":"20:00"}],"rules":["کلاه شنا الزامی است"]}`)
	writeFixture(t, dataDir, "pools.json", `{"pools":[{"slug":"main","name":"استخر اصلی","description":"آب آرام"}]}`)
	writeFixture(t, dataDir, "programmes.json", `{"categories":[{"key":"wellness","title":"سلامت","items":[{"name":"سونا","description":"بازیابی"}]}]}`)
	writeFixture(t, dataDir, "classes.json", `{"categories":[{"key":"learn","title":"آموزش","items":[{"slug":"beginner","name":"مقدماتی","coach":"مربی","time":"شنبه","capacity":10,"price":"100 تومان","price_amount":100,"description":"آموزش"}]}]}`)
	writeFixture(t, dataDir, "events.json", `[{"slug":"race","title":"مسابقه","status":"published","state":"open","date":"2030-01-01","time":"10:00","description":"رویداد","price":"200 تومان"}]`)

	userStore := user.NewStore(db)
	sessions := auth.NewSessionStore(time.Hour)
	authHandler, err := auth.NewHandler(userStore, sessions, false)
	if err != nil {
		t.Fatalf("create auth handler: %v", err)
	}
	walletService := wallet.NewService(db)
	bookingService := booking.NewService(db, walletService, booking.Prices{FreeSwim: 100, LaneTraining: 200})
	membershipService := membership.NewService(db, walletService, membership.NewCatalog([]membership.Plan{{Slug: "monthly", Name: "ماهانه", DurationDays: 30, Price: 100}}))
	classService := classes.NewService(db, walletService, classes.NewCatalog([]classes.Definition{{Slug: "beginner", Name: "مقدماتی", PriceAmount: 100}}))
	capacity := int64(10)
	eventService := events.NewService(db, walletService, events.NewCatalog([]events.Definition{{Slug: "race", Title: "مسابقه", State: "open", Price: 200, Capacity: &capacity}}))
	webHandler, err := NewHandler(Dependencies{
		Auth: authHandler, Users: userStore, Wallet: walletService, Bookings: bookingService,
		Memberships: membershipService, Classes: classService, Events: eventService,
		Rankings: rankings, DataDir: dataDir, StaticDir: filepath.Join("..", "..", "app", "static"),
	})
	if err != nil {
		t.Fatalf("create web handler: %v", err)
	}
	mux := http.NewServeMux()
	authHandler.RegisterRoutes(mux)
	webHandler.RegisterRoutes(mux)
	return webEnvironment{handler: mux, users: userStore, auth: authHandler}
}

func assertLocalStaticReferencesExist(t *testing.T, repositoryRoot string, rendered string) {
	t.Helper()
	pattern := regexp.MustCompile(`(?:src|href)="/static/([^"]+)"`)
	for _, match := range pattern.FindAllStringSubmatch(rendered, -1) {
		path := filepath.Join(repositoryRoot, "app", "static", filepath.FromSlash(match[1]))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("rendered local static reference %q is unavailable: %v", match[0], err)
		}
	}
}

func performRequest(handler http.Handler, method string, target string, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func responseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("response did not include %s cookie", name)
	return nil
}

func writeFixture(t *testing.T, directory string, name string, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func mustHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return hash
}

func sampleRankings() info.Rankings {
	return info.Rankings{Men: []info.Ranking{{Rank: "1", Name: "Swimmer", Club: "Club", Event: "100m Free", Time: "49.10", Score: "900"}}, UpdatedAt: "2030-01-01T10:00:00Z"}
}
