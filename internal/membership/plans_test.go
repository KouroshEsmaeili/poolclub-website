package membership

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPlansPreservesConfiguredValuesAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memberships.json")
	contents := []byte(`{
		"plans": [
			{"slug":"monthly","name":"ماهانه","duration_days":30,"price":50000,"tag":"محبوب"},
			{"slug":"annual","name":"سالانه","duration_days":"365","price":"400000"}
		]
	}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write temporary plans: %v", err)
	}

	plans, err := LoadPlans(path).Plans()
	if err != nil {
		t.Fatalf("Plans() error = %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("plan count = %d, want 2", len(plans))
	}
	if plans[0].Slug != "monthly" || plans[0].DurationDays != 30 || plans[0].Price != 50000 || plans[0].Tag != "محبوب" {
		t.Fatalf("first plan = %+v", plans[0])
	}
	if plans[1].Slug != "annual" || plans[1].DurationDays != 365 || plans[1].Price != 400000 {
		t.Fatalf("second plan = %+v", plans[1])
	}
}

func TestLoadPlansHasNoFallbackForMissingOrInvalidFile(t *testing.T) {
	tests := []struct {
		name string
		path func(*testing.T) string
	}{
		{
			name: "missing",
			path: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.json") },
		},
		{
			name: "invalid JSON",
			path: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "invalid.json")
				if err := os.WriteFile(path, []byte(`{"plans":`), 0o600); err != nil {
					t.Fatalf("write invalid plans: %v", err)
				}
				return path
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans, err := LoadPlans(test.path(t)).Plans()
			if !errors.Is(err, ErrPlansUnavailable) {
				t.Fatalf("Plans() error = %v, want ErrPlansUnavailable", err)
			}
			if plans != nil {
				t.Fatalf("plans = %v, want nil", plans)
			}
		})
	}
}

func TestMalformedPlanNumbersRemainInvalidWithoutInventedDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memberships.json")
	if err := os.WriteFile(path, []byte(`{"plans":[{"slug":"broken","duration_days":"days","price":null}]}`), 0o600); err != nil {
		t.Fatalf("write plans: %v", err)
	}
	plans, err := LoadPlans(path).Plans()
	if err != nil {
		t.Fatalf("Plans() error = %v", err)
	}
	if len(plans) != 1 || plans[0].DurationDays != 0 || plans[0].Price != 0 {
		t.Fatalf("plans = %+v, want invalid zero values", plans)
	}
}
