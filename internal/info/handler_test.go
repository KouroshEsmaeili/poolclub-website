package info

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KouroshEsmaeili/poolclub-website/internal/httpapi"
)

func TestPoolsAndProgrammesPreserveRawJSONShapesAndOrdering(t *testing.T) {
	directory := t.TempDir()
	poolsPath := filepath.Join(directory, "pools.json")
	programmesPath := filepath.Join(directory, "programmes.json")
	writeInfoFixture(t, poolsPath, append([]byte{0xef, 0xbb, 0xbf}, []byte(`[{"slug":"second","lanes":8},{"slug":"first","lanes":4}]`)...))
	writeInfoFixture(t, programmesPath, []byte(`{"categories":[{"key":"learn"},{"key":"compete"}]}`))
	server := newInfoServer(t, poolsPath, programmesPath, rankingFixture)

	pools := getInfoEndpoint(t, server.Client(), server.URL+"/api/pools")
	if pools.status != http.StatusOK {
		t.Fatalf("pools status = %d, want 200", pools.status)
	}
	var decodedPools []map[string]any
	if err := json.Unmarshal(pools.body, &decodedPools); err != nil {
		t.Fatalf("decode pools: %v", err)
	}
	if len(decodedPools) != 2 || decodedPools[0]["slug"] != "second" || decodedPools[1]["slug"] != "first" {
		t.Fatalf("pool order = %+v", decodedPools)
	}

	programmes := getInfoEndpoint(t, server.Client(), server.URL+"/api/programmes")
	if programmes.status != http.StatusOK {
		t.Fatalf("programmes status = %d, want 200", programmes.status)
	}
	var decodedProgrammes map[string][]map[string]any
	if err := json.Unmarshal(programmes.body, &decodedProgrammes); err != nil {
		t.Fatalf("decode programmes: %v", err)
	}
	if len(decodedProgrammes["categories"]) != 2 || decodedProgrammes["categories"][0]["key"] != "learn" || decodedProgrammes["categories"][1]["key"] != "compete" {
		t.Fatalf("programme order = %+v", decodedProgrammes)
	}
}

func TestLocalJSONEndpointsReturnSafeErrorsForMissingAndInvalidData(t *testing.T) {
	directory := t.TempDir()
	missingPath := filepath.Join(directory, "missing.json")
	invalidPath := filepath.Join(directory, "invalid.json")
	writeInfoFixture(t, invalidPath, []byte(`{"broken":`))
	server := newInfoServer(t, missingPath, invalidPath, rankingFixture)

	for _, endpoint := range []string{"/api/pools", "/api/programmes"} {
		response := getInfoEndpoint(t, server.Client(), server.URL+endpoint)
		if response.status != http.StatusInternalServerError {
			t.Fatalf("%s status = %d, want 500", endpoint, response.status)
		}
		var body map[string]any
		if err := json.Unmarshal(response.body, &body); err != nil {
			t.Fatalf("decode %s error response: %v", endpoint, err)
		}
		if body["status"] != "error" || body["message"] != "خطا در بارگذاری اطلاعات." {
			t.Fatalf("%s body = %v", endpoint, body)
		}
		if strings.Contains(string(response.body), directory) {
			t.Fatalf("%s leaked local path: %s", endpoint, response.body)
		}
	}
}

func TestLiveRankingsResponseUsesIntendedFlaskAPIStructureAndScraperFields(t *testing.T) {
	directory := t.TempDir()
	poolsPath := filepath.Join(directory, "pools.json")
	programmesPath := filepath.Join(directory, "programmes.json")
	writeInfoFixture(t, poolsPath, []byte(`[]`))
	writeInfoFixture(t, programmesPath, []byte(`[]`))
	server := newInfoServer(t, poolsPath, programmesPath, rankingFixture)

	response := getInfoEndpoint(t, server.Client(), server.URL+"/api/live-rankings")
	if response.status != http.StatusOK {
		t.Fatalf("live rankings status = %d body %s", response.status, response.body)
	}
	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal(response.body, &topLevel); err != nil {
		t.Fatalf("decode top-level rankings response: %v", err)
	}
	if len(topLevel) != 3 || topLevel["status"] == nil || topLevel["updated_at"] == nil || topLevel["items"] == nil {
		t.Fatalf("top-level response fields = %v, want status/updated_at/items", topLevel)
	}
	var body struct {
		Status    string           `json:"status"`
		UpdatedAt string           `json:"updated_at"`
		Items     []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatalf("decode rankings response: %v", err)
	}
	if body.Status != "success" || body.UpdatedAt == "" || len(body.Items) != 3 {
		t.Fatalf("rankings response = %+v", body)
	}
	wantFields := []string{"club", "event", "name", "rank", "score", "time"}
	gotFields := make([]string, 0, len(body.Items[0]))
	for field := range body.Items[0] {
		gotFields = append(gotFields, field)
	}
	// encoding/json maps are unordered, so compare membership rather than iteration order.
	for _, field := range wantFields {
		if _, ok := body.Items[0][field]; !ok {
			t.Fatalf("ranking item fields = %v, missing %q", gotFields, field)
		}
	}
	if len(body.Items[0]) != len(wantFields) {
		t.Fatalf("ranking item fields = %v, want exactly %v", gotFields, wantFields)
	}
	if body.Items[0]["name"] != "Luka Mijatovic" || body.Items[2]["name"] != "Woman Swimmer" {
		t.Fatalf("flattened men/women order = %+v", body.Items)
	}
}

func TestLiveRankingsEndpointHidesUpstreamErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "secret upstream failure", http.StatusInternalServerError)
	}))
	t.Cleanup(upstream.Close)
	handler := NewHandler("unused", "unused", NewRankingsClient(upstream.Client(), upstream.URL))
	server := httptest.NewServer(httpapi.NewRouter(handler))
	t.Cleanup(server.Close)

	response := getInfoEndpoint(t, server.Client(), server.URL+"/api/live-rankings")
	if response.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.status)
	}
	var body map[string]any
	if err := json.Unmarshal(response.body, &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["status"] != "error" || body["message"] != "خطا در دریافت رده‌بندی زنده." {
		t.Fatalf("body = %v", body)
	}
	if strings.Contains(string(response.body), "secret") {
		t.Fatalf("upstream detail leaked: %s", response.body)
	}
}

func TestReadOnlyAPIManualFlowUsesOnlyTemporaryDataAndMockUpstream(t *testing.T) {
	directory := t.TempDir()
	poolsPath := filepath.Join(directory, "pools.json")
	programmesPath := filepath.Join(directory, "programmes.json")
	writeInfoFixture(t, poolsPath, []byte(`[{"name":"Temporary pool"}]`))
	writeInfoFixture(t, programmesPath, []byte(`[{"name":"Temporary programme"}]`))
	server := newInfoServer(t, poolsPath, programmesPath, rankingFixture)

	for _, endpoint := range []string{"/api/pools", "/api/programmes", "/api/live-rankings"} {
		response := getInfoEndpoint(t, server.Client(), server.URL+endpoint)
		if response.status != http.StatusOK || !json.Valid(response.body) {
			t.Fatalf("%s response = status %d body %s", endpoint, response.status, response.body)
		}
	}
}

func newInfoServer(t *testing.T, poolsPath string, programmesPath string, upstreamHTML string) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(upstreamHTML))
	}))
	t.Cleanup(upstream.Close)
	rankings := NewRankingsClient(upstream.Client(), upstream.URL)
	rankings.now = func() time.Time { return time.Date(2030, time.March, 4, 5, 6, 7, 0, time.UTC) }
	server := httptest.NewServer(httpapi.NewRouter(NewHandler(poolsPath, programmesPath, rankings)))
	t.Cleanup(server.Close)
	return server
}

func writeInfoFixture(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", path, err)
	}
}

type infoEndpointResponse struct {
	status int
	body   []byte
}

func getInfoEndpoint(t *testing.T, client *http.Client, url string) infoEndpointResponse {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return infoEndpointResponse{status: response.StatusCode, body: body}
}
