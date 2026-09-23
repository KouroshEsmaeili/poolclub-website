package classes

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCatalogFlattensCategoriesAndPreservesEnrollmentFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "classes.json")
	contents := []byte(`{
		"categories": [
			{"key":"swim","items":[
				{"slug":"beginner","name":"مبتدی","coach":"مربی یک","time":"شنبه","capacity":1,"price_amount":30000}
			]},
			{"key":"fitness","items":[
				{"slug":"dryland","name":"بدنسازی","time":null,"capacity":"12","price_amount":"45000"}
			]}
		]
	}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temporary catalog: %v", err)
	}

	catalog := LoadCatalog(path)
	definitions, err := catalog.Classes()
	if err != nil {
		t.Fatalf("Classes() error = %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("class count = %d, want 2", len(definitions))
	}
	first := definitions[0]
	if first.Slug != "beginner" || first.Name != "مبتدی" || valueOrEmpty(first.Coach) != "مربی یک" || valueOrEmpty(first.Time) != "شنبه" || first.Capacity != 1 || first.PriceAmount != 30000 {
		t.Fatalf("first class = %+v", first)
	}
	second := definitions[1]
	if second.Slug != "dryland" || second.Coach == nil || *second.Coach != "" || second.Time != nil || second.Capacity != 12 || second.PriceAmount != 45000 {
		t.Fatalf("second class = %+v", second)
	}

	*definitions[0].Coach = "changed"
	again, err := catalog.Classes()
	if err != nil || valueOrEmpty(again[0].Coach) != "مربی یک" {
		t.Fatalf("catalog was mutated through returned data: %+v, error %v", again, err)
	}
}

func TestLoadCatalogHasNoFallbackForMissingOrInvalidFile(t *testing.T) {
	tests := []struct {
		name string
		path func(*testing.T) string
	}{
		{name: "missing", path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.json") }},
		{name: "invalid JSON", path: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "invalid.json")
			if err := os.WriteFile(path, []byte(`{"categories":`), 0o600); err != nil {
				t.Fatalf("write invalid catalog: %v", err)
			}
			return path
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions, err := LoadCatalog(test.path(t)).Classes()
			if !errors.Is(err, ErrCatalogUnavailable) {
				t.Fatalf("Classes() error = %v, want ErrCatalogUnavailable", err)
			}
			if definitions != nil {
				t.Fatalf("classes = %v, want nil", definitions)
			}
		})
	}
}

func TestMalformedCatalogNumbersRemainInvalidWithoutDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "classes.json")
	if err := os.WriteFile(path, []byte(`{"categories":[{"items":[{"slug":"broken","capacity":"many","price_amount":null}]}]}`), 0o600); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	definitions, err := LoadCatalog(path).Classes()
	if err != nil {
		t.Fatalf("Classes() error = %v", err)
	}
	if len(definitions) != 1 || definitions[0].Capacity != 0 || definitions[0].PriceAmount != 0 {
		t.Fatalf("classes = %+v, want invalid zero values", definitions)
	}
}

func TestCatalogLookupUsesFirstMatchingSlug(t *testing.T) {
	firstCoach := "first"
	secondCoach := "second"
	catalog := NewCatalog([]Definition{
		{Slug: "duplicate", Name: "first", Coach: &firstCoach, PriceAmount: 1},
		{Slug: "duplicate", Name: "second", Coach: &secondCoach, PriceAmount: 2},
	})
	found, ok, err := catalog.find("duplicate")
	if err != nil || !ok || found.Name != "first" || found.PriceAmount != 1 {
		t.Fatalf("find() = %+v, %v, %v; want first match", found, ok, err)
	}
}
