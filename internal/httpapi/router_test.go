package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtectUnsafeRequests(t *testing.T) {
	target := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := protectUnsafeRequests(target)

	tests := []struct {
		name       string
		method     string
		origin     string
		referer    string
		fetchSite  string
		wantStatus int
	}{
		{name: "safe get allows cross origin metadata", method: http.MethodGet, origin: "https://evil.example", wantStatus: http.StatusNoContent},
		{name: "same origin post", method: http.MethodPost, origin: "https://pool.example", wantStatus: http.StatusNoContent},
		{name: "cross origin post", method: http.MethodPost, origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{name: "cross site fetch metadata", method: http.MethodPost, fetchSite: "cross-site", wantStatus: http.StatusForbidden},
		{name: "same origin referer", method: http.MethodPost, referer: "https://pool.example/dashboard", wantStatus: http.StatusNoContent},
		{name: "cross origin referer", method: http.MethodPost, referer: "https://evil.example/form", wantStatus: http.StatusForbidden},
		{name: "non browser api client", method: http.MethodPost, wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "https://pool.example/test", nil)
			request.Host = "pool.example"
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.referer != "" {
				request.Header.Set("Referer", test.referer)
			}
			if test.fetchSite != "" {
				request.Header.Set("Sec-Fetch-Site", test.fetchSite)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}
