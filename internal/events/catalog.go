package events

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrCatalogUnavailable means the event configuration could not be loaded.
var ErrCatalogUnavailable = errors.New("event catalog unavailable")

// Definition contains the registration fields from one published event.
type Definition struct {
	Slug     string
	Title    string
	State    string
	Price    int64
	Capacity *int64
}

// Catalog holds an immutable, file-ordered snapshot of published events.
type Catalog struct {
	events  []Definition
	loadErr error
}

// NewCatalog creates a catalog from explicit application or test data.
func NewCatalog(events []Definition) Catalog {
	return Catalog{events: cloneDefinitions(events)}
}

// LoadCatalog reads the event catalogue. Only events whose
// status is exactly "published" are available through the APIs.
func LoadCatalog(path string) Catalog {
	contents, err := os.ReadFile(path)
	if err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)}
	}

	var configured []rawDefinition
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	if err := json.Unmarshal(contents, &configured); err != nil {
		return Catalog{loadErr: fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)}
	}

	events := make([]Definition, 0, len(configured))
	for _, raw := range configured {
		if raw.Status != "published" {
			continue
		}
		events = append(events, Definition{
			Slug:     raw.Slug,
			Title:    raw.Title,
			State:    raw.State,
			Price:    parseDisplayedPrice(raw.Price),
			Capacity: parseCapacity(raw.Capacity),
		})
	}
	return NewCatalog(events)
}

// Events returns published events in source-file order.
func (c Catalog) Events() ([]Definition, error) {
	if c.loadErr != nil {
		return nil, c.loadErr
	}
	return cloneDefinitions(c.events), nil
}

func (c Catalog) find(slug string) (Definition, bool, error) {
	if c.loadErr != nil {
		return Definition{}, false, c.loadErr
	}
	for _, event := range c.events {
		if event.Slug == slug {
			return cloneDefinition(event), true, nil
		}
	}
	return Definition{}, false, nil
}

type rawDefinition struct {
	Slug     string          `json:"slug"`
	Title    string          `json:"title"`
	Status   string          `json:"status"`
	State    string          `json:"state"`
	Price    json.RawMessage `json:"price"`
	Capacity json.RawMessage `json:"capacity"`
}

// parseDisplayedPrice uses the established digits-only parser. Invalid or absent
// prices become zero and therefore represent free authenticated registration.
func parseDisplayedPrice(raw json.RawMessage) int64 {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return 0
	}
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0
		}
	}

	var digits strings.Builder
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
			digits.WriteRune(character)
		case character >= '\u0660' && character <= '\u0669':
			digits.WriteByte(byte('0' + character - '\u0660'))
		case character >= '\u06f0' && character <= '\u06f9':
			digits.WriteByte(byte('0' + character - '\u06f0'))
		}
	}
	if digits.Len() == 0 {
		return 0
	}
	price, err := strconv.ParseInt(digits.String(), 10, 64)
	if err != nil {
		return 0
	}
	return price
}

// parseCapacity uses best-effort integer conversion. Invalid, absent,
// or null values disable capacity enforcement; zero and negative values remain
// configured capacities and therefore make the event immediately full.
func parseCapacity(raw json.RawMessage) *int64 {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return nil
	}
	if value == "true" {
		capacity := int64(1)
		return &capacity
	}
	if value == "false" {
		capacity := int64(0)
		return &capacity
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil
		}
		parsed, err := strconv.ParseInt(normalizeConfiguredDigits(strings.TrimSpace(value)), 10, 64)
		if err != nil {
			return nil
		}
		return &parsed
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	capacity := int64(parsed)
	return &capacity
}

func normalizeConfiguredDigits(value string) string {
	var normalized strings.Builder
	for _, character := range value {
		switch {
		case character >= '\u0660' && character <= '\u0669':
			normalized.WriteByte(byte('0' + character - '\u0660'))
		case character >= '\u06f0' && character <= '\u06f9':
			normalized.WriteByte(byte('0' + character - '\u06f0'))
		default:
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		cloned = append(cloned, cloneDefinition(definition))
	}
	return cloned
}

func cloneDefinition(definition Definition) Definition {
	if definition.Capacity != nil {
		capacity := *definition.Capacity
		definition.Capacity = &capacity
	}
	return definition
}
