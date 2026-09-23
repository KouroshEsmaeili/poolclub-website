package booking

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPricesUsesDefaultsWhenFileIsMissingOrInvalid(t *testing.T) {
	missing := LoadPrices(filepath.Join(t.TempDir(), "missing.json"))
	if missing != DefaultPrices() {
		t.Fatalf("missing-file prices = %+v, want %+v", missing, DefaultPrices())
	}

	invalidPath := filepath.Join(t.TempDir(), "prices.json")
	if err := os.WriteFile(invalidPath, []byte("not json"), 0o600); err != nil {
		t.Fatalf("write invalid prices: %v", err)
	}
	invalid := LoadPrices(invalidPath)
	if invalid != DefaultPrices() {
		t.Fatalf("invalid-file prices = %+v, want %+v", invalid, DefaultPrices())
	}
}

func TestLoadPricesUsesConfiguredValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prices.json")
	if err := os.WriteFile(path, []byte(`{"free_swim":45000,"lane_training":90000}`), 0o600); err != nil {
		t.Fatalf("write prices: %v", err)
	}
	prices := LoadPrices(path)
	if prices.FreeSwim != 45000 || prices.LaneTraining != 90000 {
		t.Fatalf("prices = %+v, want free_swim=45000 lane_training=90000", prices)
	}
}
