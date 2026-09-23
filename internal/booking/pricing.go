package booking

import (
	"encoding/json"
	"os"
)

const (
	DefaultFreeSwimPrice     int64 = 40000
	DefaultLaneTrainingPrice int64 = 80000
)

// Prices contains the flat per-booking prices used by the booking API.
type Prices struct {
	FreeSwim     int64 `json:"free_swim"`
	LaneTraining int64 `json:"lane_training"`
}

// DefaultPrices returns the fallback booking prices.
func DefaultPrices() Prices {
	return Prices{
		FreeSwim:     DefaultFreeSwimPrice,
		LaneTraining: DefaultLaneTrainingPrice,
	}
}

// LoadPrices loads prices.json and falls back to the established defaults
// when the file is missing or invalid.
func LoadPrices(path string) Prices {
	prices := DefaultPrices()
	contents, err := os.ReadFile(path)
	if err != nil {
		return prices
	}

	var configured struct {
		FreeSwim     *int64 `json:"free_swim"`
		LaneTraining *int64 `json:"lane_training"`
	}
	if err := json.Unmarshal(contents, &configured); err != nil {
		return prices
	}
	if configured.FreeSwim != nil {
		prices.FreeSwim = *configured.FreeSwim
	}
	if configured.LaneTraining != nil {
		prices.LaneTraining = *configured.LaneTraining
	}
	return prices
}
