package events

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogKeepsPublishedEventsAndParsesFlaskFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	contents := append([]byte{0xef, 0xbb, 0xbf}, []byte(`[
		{"slug":"paid","title":"مسابقه","status":"published","state":"open","price":"150,000 تومان","capacity":"۲"},
		{"slug":"draft","title":"پیش‌نویس","status":"draft","state":"open","price":"100","capacity":9},
		{"slug":"free","title":"رایگان","status":"published","state":"closed","price":"رایگان","capacity":null},
		{"slug":"persian","title":"فارسی","status":"published","state":"open","price":"۱۲٬۳۴۵ تومان","capacity":3.9}
	]`)...)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temporary catalog: %v", err)
	}

	catalog := LoadCatalog(path)
	events, err := catalog.Events()
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("published event count = %d, want 3", len(events))
	}
	if events[0].Slug != "paid" || events[0].Title != "مسابقه" || events[0].State != "open" || events[0].Price != 150000 || events[0].Capacity == nil || *events[0].Capacity != 2 {
		t.Fatalf("paid event = %+v", events[0])
	}
	if events[1].Slug != "free" || events[1].Price != 0 || events[1].Capacity != nil {
		t.Fatalf("free event = %+v", events[1])
	}
	if events[2].Price != 12345 || events[2].Capacity == nil || *events[2].Capacity != 3 {
		t.Fatalf("Persian/float fields = %+v", events[2])
	}

	*events[0].Capacity = 99
	again, err := catalog.Events()
	if err != nil || again[0].Capacity == nil || *again[0].Capacity != 2 {
		t.Fatalf("catalog mutated through returned data: %+v, error %v", again, err)
	}
}

func TestLoadCatalogHasNoFallbackForMissingOrMalformedFile(t *testing.T) {
	tests := []struct {
		name string
		path func(*testing.T) string
	}{
		{name: "missing", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.json") }},
		{name: "malformed", path: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "events.json")
			if err := os.WriteFile(path, []byte(`[{"slug":`), 0o600); err != nil {
				t.Fatalf("write malformed catalog: %v", err)
			}
			return path
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := LoadCatalog(test.path(t)).Events()
			if !errors.Is(err, ErrCatalogUnavailable) {
				t.Fatalf("Events() error = %v, want ErrCatalogUnavailable", err)
			}
			if events != nil {
				t.Fatalf("events = %v, want nil", events)
			}
		})
	}
}

func TestCatalogMatchesFlaskFirstPublishedSlugAndInvalidCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.json")
	contents := []byte(`[
		{"slug":"same","title":"draft first","status":"draft","state":"open","price":"1","capacity":1},
		{"slug":"same","title":"published first","status":"published","state":"open","price":"2","capacity":"invalid"},
		{"slug":"same","title":"published second","status":"published","state":"open","price":"3","capacity":3}
	]`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}

	found, ok, err := LoadCatalog(path).find("same")
	if err != nil || !ok || found.Title != "published first" || found.Price != 2 || found.Capacity != nil {
		t.Fatalf("find() = %+v, %v, %v", found, ok, err)
	}
}
