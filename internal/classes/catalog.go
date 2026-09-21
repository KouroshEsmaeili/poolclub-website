package classes

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrCatalogUnavailable means the Flask class configuration could not be loaded.
var ErrCatalogUnavailable = errors.New("class catalog unavailable")

// Definition contains the authoritative enrollment fields from data/classes.json.
type Definition struct {
	Slug        string
	Name        string
	Coach       *string
	Time        *string
	PriceAmount int64
	Capacity    int64
}

// Catalog holds an immutable, file-ordered snapshot of configured classes.
type Catalog struct {
	classes []Definition
	loadErr error
}

// NewCatalog creates a catalog from explicit application or test data.
func NewCatalog(classes []Definition) Catalog {
	return Catalog{classes: cloneDefinitions(classes)}
}

// LoadCatalog reads the same class file used by Flask. Flask has no fallback
// classes, so a missing or invalid file remains an explicit error.
func LoadCatalog(path string) Catalog {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)}
	}

	var configured struct {
		Categories []struct {
			Items []rawDefinition `json:"items"`
		} `json:"categories"`
	}
	if err := json.Unmarshal(contents, &configured); err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)}
	}

	definitions := make([]Definition, 0)
	for _, category := range configured.Categories {
		for _, raw := range category.Items {
			definitions = append(definitions, Definition{
				Slug:        raw.Slug,
				Name:        raw.Name,
				Coach:       configuredOptionalString(raw.Coach),
				Time:        configuredOptionalString(raw.Time),
				PriceAmount: parseConfiguredInteger(raw.PriceAmount),
				Capacity:    parseConfiguredInteger(raw.Capacity),
			})
		}
	}
	return NewCatalog(definitions)
}

// Classes returns configured classes in category and item file order.
func (c Catalog) Classes() ([]Definition, error) {
	if c.loadErr != nil {
		return nil, c.loadErr
	}
	return cloneDefinitions(c.classes), nil
}

func (c Catalog) find(slug string) (Definition, bool, error) {
	if c.loadErr != nil {
		return Definition{}, false, c.loadErr
	}
	for _, class := range c.classes {
		if class.Slug == slug {
			return cloneDefinition(class), true, nil
		}
	}
	return Definition{}, false, nil
}

type rawDefinition struct {
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Coach       json.RawMessage `json:"coach"`
	Time        json.RawMessage `json:"time"`
	PriceAmount json.RawMessage `json:"price_amount"`
	Capacity    json.RawMessage `json:"capacity"`
}

func configuredOptionalString(raw json.RawMessage) *string {
	if len(raw) == 0 {
		empty := ""
		return &empty
	}
	if strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		empty := ""
		return &empty
	}
	return &value
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

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		cloned = append(cloned, cloneDefinition(definition))
	}
	return cloned
}

func cloneDefinition(definition Definition) Definition {
	definition.Coach = cloneString(definition.Coach)
	definition.Time = cloneString(definition.Time)
	return definition
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
