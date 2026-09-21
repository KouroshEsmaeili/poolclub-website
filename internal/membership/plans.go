package membership

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrPlansUnavailable means the Flask membership configuration could not be loaded.
var ErrPlansUnavailable = errors.New("membership plans unavailable")

// Plan is one purchasable membership plan from data/memberships.json.
type Plan struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	DurationDays    int64  `json:"duration_days"`
	Price           int64  `json:"price"`
	Tag             string `json:"tag,omitempty"`
	BadgeClass      string `json:"badge_class,omitempty"`
	Description     string `json:"description,omitempty"`
	DescriptionLong string `json:"description_long,omitempty"`
}

// Catalog holds an immutable snapshot of configured membership plans.
type Catalog struct {
	plans   []Plan
	loadErr error
}

// NewCatalog creates a catalog from already validated application data.
func NewCatalog(plans []Plan) Catalog {
	return Catalog{plans: append([]Plan(nil), plans...)}
}

// LoadPlans reads the same membership plan file used by Flask. Flask has no
// fallback plans, so a missing or invalid file remains an explicit error.
func LoadPlans(path string) Catalog {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrPlansUnavailable, err)}
	}

	var configured struct {
		Plans []rawPlan `json:"plans"`
	}
	if err := json.Unmarshal(contents, &configured); err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrPlansUnavailable, err)}
	}

	plans := make([]Plan, 0, len(configured.Plans))
	for _, raw := range configured.Plans {
		plans = append(plans, Plan{
			Slug:            raw.Slug,
			Name:            raw.Name,
			DurationDays:    parseConfiguredInteger(raw.DurationDays),
			Price:           parseConfiguredInteger(raw.Price),
			Tag:             raw.Tag,
			BadgeClass:      raw.BadgeClass,
			Description:     raw.Description,
			DescriptionLong: raw.DescriptionLong,
		})
	}
	return NewCatalog(plans)
}

// Plans returns configured plans in file order.
func (c Catalog) Plans() ([]Plan, error) {
	if c.loadErr != nil {
		return nil, c.loadErr
	}
	return append([]Plan(nil), c.plans...), nil
}

func (c Catalog) find(slug string) (Plan, bool, error) {
	if c.loadErr != nil {
		return Plan{}, false, c.loadErr
	}
	for _, plan := range c.plans {
		if plan.Slug == slug {
			return plan, true, nil
		}
	}
	return Plan{}, false, nil
}

type rawPlan struct {
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	DurationDays    json.RawMessage `json:"duration_days"`
	Price           json.RawMessage `json:"price"`
	Tag             string          `json:"tag"`
	BadgeClass      string          `json:"badge_class"`
	Description     string          `json:"description"`
	DescriptionLong string          `json:"description_long"`
}

func parseConfiguredInteger(raw json.RawMessage) int64 {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return 0
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0
		}
		parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return int64(parsed)
}
