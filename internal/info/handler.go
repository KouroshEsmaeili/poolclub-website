package info

import (
	"context"
	"encoding/json"
	"net/http"
)

// RankingsFetcher supplies live rankings to the HTTP handler.
type RankingsFetcher interface {
	Fetch(context.Context) (Rankings, error)
}

// Handler serves the read-only information APIs.
type Handler struct {
	poolsPath      string
	programmesPath string
	rankings       RankingsFetcher
}

// NewHandler creates a read-only information handler.
func NewHandler(poolsPath string, programmesPath string, rankings RankingsFetcher) *Handler {
	return &Handler{poolsPath: poolsPath, programmesPath: programmesPath, rankings: rankings}
}

// RegisterRoutes adds the read-only routes to a mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/pools", h.pools)
	mux.HandleFunc("GET /api/programmes", h.programmes)
	mux.HandleFunc("GET /api/live-rankings", h.liveRankings)
}

func (h *Handler) pools(w http.ResponseWriter, _ *http.Request) {
	h.writeLocalJSON(w, h.poolsPath)
}

func (h *Handler) programmes(w http.ResponseWriter, _ *http.Request) {
	h.writeLocalJSON(w, h.programmesPath)
}

func (h *Handler) writeLocalJSON(w http.ResponseWriter, path string) {
	contents, err := loadJSONFile(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "خطا در بارگذاری اطلاعات.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(contents)
}

func (h *Handler) liveRankings(w http.ResponseWriter, r *http.Request) {
	rankings, err := h.rankings.Fetch(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "خطا در دریافت رده‌بندی زنده.")
		return
	}
	// The API exposes one "items" array, while the scraper
	// returns separate men and women lists. Flattening them in scraper order
	// repairs the route's tuple-unpacking bug without inventing a new API shape.
	items := make([]Ranking, 0, len(rankings.Men)+len(rankings.Women))
	items = append(items, rankings.Men...)
	items = append(items, rankings.Women...)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"updated_at": rankings.UpdatedAt,
		"items":      items,
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"status": "error", "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
