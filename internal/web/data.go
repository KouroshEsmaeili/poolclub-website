package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type siteConfig struct {
	Brand   string `json:"brand"`
	Tagline string `json:"tagline"`
	City    string `json:"city"`
	Address string `json:"address"`
	Email   string `json:"email"`
	Phone   string `json:"phone"`
	Map     struct {
		EmbedURL string `json:"embed_url"`
	} `json:"map"`
	Social struct {
		Instagram string `json:"instagram"`
		Telegram  string `json:"telegram"`
		GitHub    string `json:"github"`
	} `json:"social"`
}

type hoursConfig struct {
	Timezone string `json:"timezone"`
	Weekly   []struct {
		Day   string `json:"dow"`
		Open  string `json:"open"`
		Close string `json:"close"`
	} `json:"weekly"`
	Rules []string `json:"rules"`
}

type facility struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	Depth       string `json:"depth"`
	Length      string `json:"length"`
	Temperature string `json:"temperature"`
	SuitableFor string `json:"suitable_for"`
}

type poolsConfig struct {
	Pools []facility `json:"pools"`
}

type programmeCategory struct {
	Key   string     `json:"key"`
	Title string     `json:"title"`
	Items []facility `json:"items"`
}

type programmesConfig struct {
	Categories []programmeCategory `json:"categories"`
}

type classItem struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Coach       string   `json:"coach"`
	Time        string   `json:"time"`
	Capacity    any      `json:"capacity"`
	Price       string   `json:"price"`
	PriceAmount any      `json:"price_amount"`
	Description string   `json:"description"`
	Image       string   `json:"image"`
	Tags        []string `json:"tags"`
}

type classCategory struct {
	Key   string      `json:"key"`
	Title string      `json:"title"`
	Items []classItem `json:"items"`
}

type classesConfig struct {
	Categories []classCategory `json:"categories"`
}

type eventItem struct {
	Slug            string   `json:"slug"`
	Title           string   `json:"title"`
	Status          string   `json:"status"`
	State           string   `json:"state"`
	Type            string   `json:"type"`
	Date            string   `json:"date"`
	Time            string   `json:"time"`
	Description     string   `json:"description"`
	Audience        string   `json:"audience"`
	Price           any      `json:"price"`
	Image           string   `json:"image"`
	Tags            []string `json:"tags"`
	RegisteredCount int64
	UserRegistered  bool
}

func loadJSON(dataDir string, name string, destination any) error {
	contents, err := os.ReadFile(filepath.Join(dataDir, name))
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	contents = bytes.TrimPrefix(contents, []byte{0xef, 0xbb, 0xbf})
	if err := json.Unmarshal(contents, destination); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}
