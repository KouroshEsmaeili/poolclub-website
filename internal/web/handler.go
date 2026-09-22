// Package web serves the browser-facing HTML and existing static frontend.
package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/auth"
	"github.com/KouroshEsmaeili/poolclub-website/internal/booking"
	"github.com/KouroshEsmaeili/poolclub-website/internal/classes"
	"github.com/KouroshEsmaeili/poolclub-website/internal/events"
	"github.com/KouroshEsmaeili/poolclub-website/internal/info"
	"github.com/KouroshEsmaeili/poolclub-website/internal/membership"
	"github.com/KouroshEsmaeili/poolclub-website/internal/model"
	"github.com/KouroshEsmaeili/poolclub-website/internal/user"
	"github.com/KouroshEsmaeili/poolclub-website/internal/wallet"
	"golang.org/x/crypto/bcrypt"
)

//go:embed templates/*.tmpl
var templateFiles embed.FS

type Dependencies struct {
	Auth         *auth.Handler
	Users        *user.Store
	Wallet       *wallet.Service
	Bookings     *booking.Service
	Memberships  *membership.Service
	Classes      *classes.Service
	Events       *events.Service
	Rankings     info.RankingsFetcher
	DataDir      string
	StaticDir    string
	CookieSecure bool
}

type Handler struct {
	auth        *auth.Handler
	users       *user.Store
	wallet      *wallet.Service
	bookings    *booking.Service
	memberships *membership.Service
	classes     *classes.Service
	events      *events.Service
	rankings    info.RankingsFetcher
	dataDir     string
	static      http.Handler
	templates   map[string]*template.Template
	flashes     *flashStore
}

type pageData struct {
	Site          siteConfig
	User          *model.User
	Active        string
	Flash         *flashMessage
	Next          string
	Hours         hoursConfig
	Pools         poolsConfig
	Programmes    programmesConfig
	ClassGroups   []classCategory
	Events        []eventItem
	Rankings      []info.Ranking
	UpdatedAt     string
	Wallet        wallet.Snapshot
	Current       membership.Current
	Plans         []membership.Plan
	History       []model.MembershipHistoryItem
	Bookings      []model.Booking
	Upcoming      []model.Booking
	Past          []model.Booking
	NextBooking   *model.Booking
	UpcomingCount int
	PastCount     int
	Enrollments   []model.ClassEnrollment
	Registrations []model.EventRegistration
}

func NewHandler(dependencies Dependencies) (*Handler, error) {
	if dependencies.Auth == nil || dependencies.Users == nil || dependencies.Wallet == nil || dependencies.Bookings == nil || dependencies.Memberships == nil || dependencies.Classes == nil || dependencies.Events == nil {
		return nil, errors.New("web dependencies must not be nil")
	}
	if dependencies.DataDir == "" {
		dependencies.DataDir = "data"
	}
	if dependencies.StaticDir == "" {
		dependencies.StaticDir = filepath.Join("app", "static")
	}

	h := &Handler{
		auth: dependencies.Auth, users: dependencies.Users, wallet: dependencies.Wallet,
		bookings: dependencies.Bookings, memberships: dependencies.Memberships,
		classes: dependencies.Classes, events: dependencies.Events, rankings: dependencies.Rankings,
		dataDir: dependencies.DataDir,
		static:  http.StripPrefix("/static/", http.FileServer(http.Dir(dependencies.StaticDir))),
		flashes: newFlashStore(dependencies.CookieSecure), templates: make(map[string]*template.Template),
	}
	if err := h.parseTemplates(); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /", h.home)
	mux.HandleFunc("GET /auth/login", h.loginPage)
	mux.HandleFunc("POST /auth/login", h.loginPage)
	mux.HandleFunc("GET /auth/register", h.registerPage)
	mux.HandleFunc("POST /auth/register", h.registerPage)
	mux.HandleFunc("GET /auth/logout", h.logout)
	mux.HandleFunc("GET /dashboard", h.dashboard)
	mux.HandleFunc("GET /dashboard/wallet", h.walletPage)
	mux.HandleFunc("GET /dashboard/membership", h.membershipPage)
	mux.HandleFunc("POST /dashboard/membership/buy", h.membershipBuy)
	mux.HandleFunc("POST /dashboard/membership/cancel", h.membershipCancel)
	mux.HandleFunc("GET /dashboard/bookings", h.bookingsPage)
	mux.HandleFunc("GET /dashboard/classes", h.classesPage)
	mux.HandleFunc("GET /dashboard/events", h.eventsPage)
	mux.HandleFunc("GET /dashboard/profile", h.profilePage)
	mux.HandleFunc("POST /dashboard/profile", h.profilePage)
	mux.Handle("GET /static/", h.static)
}

func (h *Handler) parseTemplates() error {
	funcs := template.FuncMap{
		"value": func(value *string) string {
			if value == nil {
				return ""
			}
			return *value
		},
		"date": func(value *time.Time) string {
			if value == nil {
				return ""
			}
			return value.Format("2006-01-02")
		},
		"day":      func(value time.Time) string { return value.Format("2006-01-02") },
		"datetime": func(value time.Time) string { return value.Format("2006-01-02 15:04") },
		"eq":       func(first, second string) bool { return first == second },
	}
	pages := map[string]string{
		"home": "base.tmpl", "login": "auth.tmpl", "register": "auth.tmpl",
		"dashboard": "dashboard_layout.tmpl", "wallet": "dashboard_layout.tmpl", "membership": "dashboard_layout.tmpl",
		"bookings": "dashboard_layout.tmpl", "classes": "dashboard_layout.tmpl", "events": "dashboard_layout.tmpl", "profile": "dashboard_layout.tmpl",
	}
	for page, layout := range pages {
		parsed, err := template.New("layout").Funcs(funcs).ParseFS(templateFiles, "templates/"+layout, "templates/partials.tmpl", "templates/"+page+".tmpl")
		if err != nil {
			return fmt.Errorf("parse %s template: %w", page, err)
		}
		h.templates[page] = parsed
	}
	return nil
}

func (h *Handler) baseData(w http.ResponseWriter, r *http.Request) pageData {
	data := pageData{Flash: h.flashes.consume(w, r)}
	_ = loadJSON(h.dataDir, "site.json", &data.Site)
	if id, ok := h.auth.Authenticate(w, r); ok {
		if found, err := h.users.FindByID(r.Context(), id); err == nil {
			data.User = found
		}
	}
	return data
}

func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) (*model.User, pageData, bool) {
	data := h.baseData(w, r)
	if data.User == nil {
		target := "/auth/login?next=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, target, http.StatusFound)
		return nil, data, false
	}
	return data.User, data, true
}

func (h *Handler) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates[name].ExecuteTemplate(w, "layout", data); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (h *Handler) serverError(w http.ResponseWriter, err error) {
	log.Printf("web request failed: %v", err)
	http.Error(w, "خطا در بارگذاری صفحه.", http.StatusInternalServerError)
}

func safeNext(raw string, fallback string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(raw, "//") || parsed.Host != "" || parsed.Scheme != "" {
		return fallback
	}
	return parsed.RequestURI()
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := h.baseData(w, r)
	var classesData classesConfig
	var configuredEvents []eventItem
	for name, destination := range map[string]any{
		"hours.json": &data.Hours, "pools.json": &data.Pools, "programmes.json": &data.Programmes,
		"classes.json": &classesData, "events.json": &configuredEvents,
	} {
		if err := loadJSON(h.dataDir, name, destination); err != nil {
			h.serverError(w, err)
			return
		}
	}
	data.ClassGroups = classesData.Categories
	for _, event := range configuredEvents {
		if event.Status != "published" {
			continue
		}
		count, err := h.events.RegisteredCount(r.Context(), event.Slug)
		if err != nil {
			h.serverError(w, err)
			return
		}
		event.RegisteredCount = count
		if data.User != nil {
			event.UserRegistered, err = h.events.UserIsRegistered(r.Context(), data.User.ID, event.Slug)
			if err != nil {
				h.serverError(w, err)
				return
			}
		}
		data.Events = append(data.Events, event)
	}
	sort.SliceStable(data.Events, func(i, j int) bool { return data.Events[i].Date < data.Events[j].Date })
	if h.rankings != nil {
		if rankingData, err := h.rankings.Fetch(r.Context()); err == nil {
			data.Rankings = append(rankingData.Men, rankingData.Women...)
			data.UpdatedAt = rankingData.UpdatedAt
		}
	}
	h.render(w, "home", data)
}

func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	data := h.baseData(w, r)
	if data.User != nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	data.Next = r.URL.Query().Get("next")
	if r.Method == http.MethodGet {
		h.render(w, "login", data)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "درخواست نامعتبر است.", http.StatusBadRequest)
		return
	}
	found, err := h.auth.AuthenticateCredentials(r, r.FormValue("email"), r.FormValue("password"))
	if errors.Is(err, user.ErrNotFound) {
		data.Flash = &flashMessage{Category: "danger", Text: "ایمیل یا رمز عبور نادرست است."}
		h.render(w, "login", data)
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}
	if err := h.auth.StartSession(w, r, found.ID); err != nil {
		h.serverError(w, err)
		return
	}
	h.flashes.set(w, flashMessage{Category: "success", Text: "با موفقیت وارد شدید."})
	http.Redirect(w, r, safeNext(r.FormValue("next"), "/dashboard"), http.StatusFound)
}

func (h *Handler) registerPage(w http.ResponseWriter, r *http.Request) {
	data := h.baseData(w, r)
	if data.User != nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	if r.Method == http.MethodGet {
		h.render(w, "register", data)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "درخواست نامعتبر است.", http.StatusBadRequest)
		return
	}
	email, password := user.NormalizeEmail(r.FormValue("email")), r.FormValue("password")
	if email == "" || password == "" {
		data.Flash = &flashMessage{Category: "danger", Text: "ایمیل و رمز عبور الزامی است."}
		h.render(w, "register", data)
		return
	}
	if password != r.FormValue("password2") {
		data.Flash = &flashMessage{Category: "danger", Text: "تکرار رمز عبور هم‌خوانی ندارد."}
		h.render(w, "register", data)
		return
	}
	created, err := h.auth.RegisterAccount(r, auth.Registration{FirstName: r.FormValue("first_name"), LastName: r.FormValue("last_name"), Email: email, Password: password})
	if errors.Is(err, user.ErrEmailExists) {
		data.Flash = &flashMessage{Category: "danger", Text: "این ایمیل قبلاً ثبت شده است."}
		h.render(w, "register", data)
		return
	}
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		data.Flash = &flashMessage{Category: "danger", Text: "رمز عبور بیش از حد طولانی است."}
		h.render(w, "register", data)
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}
	if err := h.auth.StartSession(w, r, created.ID); err != nil {
		h.serverError(w, err)
		return
	}
	h.flashes.set(w, flashMessage{Category: "success", Text: "حساب کاربری با موفقیت ساخته شد."})
	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.auth.Authenticate(w, r); !ok {
		http.Redirect(w, r, "/auth/login?next=%2Fauth%2Flogout", http.StatusFound)
		return
	}
	h.auth.EndSession(w, r)
	h.flashes.set(w, flashMessage{Category: "success", Text: "از حساب کاربری خارج شدید."})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	items, err := h.bookings.Bookings(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Bookings = items
	for _, item := range items {
		if h.bookings.IsPast(item) {
			data.PastCount++
		} else {
			data.UpcomingCount++
		}
	}
	data.NextBooking, err = h.bookings.NextReservation(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Enrollments, err = h.classes.Enrollments(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Current, err = h.memberships.CurrentMembership(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Active = "dashboard"
	h.render(w, "dashboard", data)
}

func (h *Handler) walletPage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var err error
	data.Wallet, err = h.wallet.Get(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Active = "wallet"
	h.render(w, "wallet", data)
}

func (h *Handler) membershipPage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var err error
	data.Current, err = h.memberships.CurrentMembership(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Plans, err = h.memberships.Plans()
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.History, err = h.memberships.History(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Active = "membership"
	h.render(w, "membership", data)
}

func (h *Handler) membershipBuy(w http.ResponseWriter, r *http.Request) {
	current, _, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "درخواست نامعتبر است.", http.StatusBadRequest)
		return
	}
	result, err := h.memberships.Purchase(r.Context(), current.ID, strings.TrimSpace(r.FormValue("plan_slug")))
	if err != nil {
		h.flashes.set(w, flashMessage{Category: "danger", Text: membershipError(err)})
		http.Redirect(w, r, "/dashboard/membership", http.StatusFound)
		return
	}
	h.flashes.set(w, flashMessage{Category: "success", Text: fmt.Sprintf("اشتراک با موفقیت فعال شد؛ اعتبار تا %s.", result.Membership.ExpiresAt.Format("2006-01-02"))})
	http.Redirect(w, r, "/dashboard/membership", http.StatusFound)
}

func (h *Handler) membershipCancel(w http.ResponseWriter, r *http.Request) {
	current, _, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "درخواست نامعتبر است.", http.StatusBadRequest)
		return
	}
	_, err := h.memberships.Cancel(r.Context(), current.ID, strings.TrimSpace(r.FormValue("history_id")))
	if err != nil {
		h.flashes.set(w, flashMessage{Category: "danger", Text: membershipError(err)})
		http.Redirect(w, r, "/dashboard/membership", http.StatusFound)
		return
	}
	h.flashes.set(w, flashMessage{Category: "success", Text: "اشتراک لغو شد و مبلغ به کیف پول بازگشت."})
	http.Redirect(w, r, "/dashboard/membership", http.StatusFound)
}

func membershipError(err error) string {
	switch {
	case errors.Is(err, membership.ErrInsufficientFunds):
		return "موجودی کیف پول کافی نیست."
	case errors.Is(err, membership.ErrCancellationWindow):
		return "لغو اشتراک فقط در روز خرید ممکن است."
	case errors.Is(err, membership.ErrPlanNotFound), errors.Is(err, membership.ErrInvalidPlan):
		return "طرح اشتراک نامعتبر است."
	default:
		return "انجام عملیات اشتراک ممکن نشد."
	}
}

func (h *Handler) bookingsPage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	items, err := h.bookings.Bookings(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	for _, item := range items {
		if h.bookings.IsPast(item) {
			data.Past = append(data.Past, item)
		} else {
			data.Upcoming = append(data.Upcoming, item)
		}
	}
	data.Active = "bookings"
	h.render(w, "bookings", data)
}

func (h *Handler) classesPage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var configured classesConfig
	if err := loadJSON(h.dataDir, "classes.json", &configured); err != nil {
		h.serverError(w, err)
		return
	}
	data.ClassGroups = configured.Categories
	var err error
	data.Enrollments, err = h.classes.Enrollments(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Active = "classes"
	h.render(w, "classes", data)
}

func (h *Handler) eventsPage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var configured []eventItem
	if err := loadJSON(h.dataDir, "events.json", &configured); err != nil {
		h.serverError(w, err)
		return
	}
	for _, event := range configured {
		if event.Status != "published" {
			continue
		}
		var err error
		event.RegisteredCount, err = h.events.RegisteredCount(r.Context(), event.Slug)
		if err != nil {
			h.serverError(w, err)
			return
		}
		event.UserRegistered, err = h.events.UserIsRegistered(r.Context(), current.ID, event.Slug)
		if err != nil {
			h.serverError(w, err)
			return
		}
		data.Events = append(data.Events, event)
	}
	var err error
	data.Registrations, err = h.events.Registrations(r.Context(), current.ID)
	if err != nil {
		h.serverError(w, err)
		return
	}
	data.Active = "events"
	h.render(w, "events", data)
}

func (h *Handler) profilePage(w http.ResponseWriter, r *http.Request) {
	current, data, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	data.Active = "profile"
	if r.Method == http.MethodGet {
		h.render(w, "profile", data)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "درخواست نامعتبر است.", http.StatusBadRequest)
		return
	}
	if r.FormValue("profile_action") == "password" {
		valid, _ := auth.VerifyPassword(current.PasswordHash, r.FormValue("current_password"))
		password := r.FormValue("new_password")
		if !valid {
			data.Flash = &flashMessage{Category: "danger", Text: "رمز عبور فعلی نادرست است."}
			h.render(w, "profile", data)
			return
		}
		if password != r.FormValue("new_password2") {
			data.Flash = &flashMessage{Category: "danger", Text: "تکرار رمز عبور جدید هم‌خوانی ندارد."}
			h.render(w, "profile", data)
			return
		}
		if len(password) < 6 {
			data.Flash = &flashMessage{Category: "danger", Text: "رمز عبور جدید باید حداقل ۶ کاراکتر باشد."}
			h.render(w, "profile", data)
			return
		}
		hash, err := auth.HashPassword(password)
		if err != nil {
			h.serverError(w, err)
			return
		}
		if err := h.users.UpdatePasswordHash(r.Context(), current.ID, hash); err != nil {
			h.serverError(w, err)
			return
		}
	} else {
		email := user.NormalizeEmail(r.FormValue("email"))
		first, last := strings.TrimSpace(r.FormValue("first_name")), strings.TrimSpace(r.FormValue("last_name"))
		if email == "" || first == "" || last == "" {
			data.Flash = &flashMessage{Category: "danger", Text: "ایمیل، نام و نام خانوادگی الزامی است."}
			h.render(w, "profile", data)
			return
		}
		err := h.users.UpdateProfile(r.Context(), current.ID, email, first, last, strings.TrimSpace(r.FormValue("phone")), strings.TrimSpace(r.FormValue("birthdate")), strings.TrimSpace(r.FormValue("emergency_contact")))
		if errors.Is(err, user.ErrEmailExists) {
			data.Flash = &flashMessage{Category: "danger", Text: "این ایمیل قبلاً ثبت شده است."}
			h.render(w, "profile", data)
			return
		}
		if err != nil {
			h.serverError(w, err)
			return
		}
	}
	h.flashes.set(w, flashMessage{Category: "success", Text: "تنظیمات پروفایل با موفقیت ذخیره شد."})
	http.Redirect(w, r, "/dashboard/profile", http.StatusFound)
}
