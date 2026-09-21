package info

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	scriptBlockPattern = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	styleBlockPattern  = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`)
)

const (
	defaultSwimCloudURL = "https://www.swimcloud.com/"
	defaultRankingRows  = 5
	maxRankingBodyBytes = 4 << 20
	rankingUserAgent    = "Mozilla/5.0 (compatible; PoolClubBot/1.0; +https://yourdomain.example)"
)

// Ranking is one parsed SwimCloud Top Swims table row.
type Ranking struct {
	Rank  string `json:"rank"`
	Name  string `json:"name"`
	Club  string `json:"club"`
	Event string `json:"event"`
	Time  string `json:"time"`
	Score string `json:"score"`
}

// Rankings contains the gender-separated scraper result used by Flask.
type Rankings struct {
	Men       []Ranking
	Women     []Ranking
	UpdatedAt string
}

// HTTPDoer is the minimal standard-library HTTP client contract used by the scraper.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// RankingsClient fetches and parses SwimCloud Top Swims.
type RankingsClient struct {
	httpClient HTTPDoer
	baseURL    string
	maxRows    int
	now        func() time.Time
}

// NewRankingsClient creates a testable SwimCloud client.
func NewRankingsClient(httpClient HTTPDoer, baseURL string) *RankingsClient {
	return &RankingsClient{
		httpClient: httpClient,
		baseURL:    baseURL,
		maxRows:    defaultRankingRows,
		now:        time.Now,
	}
}

// NewDefaultRankingsClient creates the production client. Flask already uses
// a ten-second request timeout; the Go port keeps that explicit bound.
func NewDefaultRankingsClient() *RankingsClient {
	return NewRankingsClient(&http.Client{Timeout: 10 * time.Second}, defaultSwimCloudURL)
}

// Fetch retrieves the USA region page and returns parsed men and women rows.
func (c *RankingsClient) Fetch(ctx context.Context) (Rankings, error) {
	requestURL, err := url.Parse(c.baseURL)
	if err != nil {
		return Rankings{}, fmt.Errorf("build SwimCloud URL: %w", err)
	}
	query := requestURL.Query()
	query.Set("r", "country_USA")
	requestURL.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return Rankings{}, fmt.Errorf("create SwimCloud request: %w", err)
	}
	request.Header.Set("User-Agent", rankingUserAgent)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return Rankings{}, fmt.Errorf("fetch SwimCloud rankings: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Rankings{}, fmt.Errorf("fetch SwimCloud rankings: unexpected status %d", response.StatusCode)
	}

	limited := io.LimitReader(response.Body, maxRankingBodyBytes+1)
	contents, err := io.ReadAll(limited)
	if err != nil {
		return Rankings{}, fmt.Errorf("read SwimCloud rankings: %w", err)
	}
	if len(contents) > maxRankingBodyBytes {
		return Rankings{}, errors.New("read SwimCloud rankings: response too large")
	}

	men, women, err := parseRankings(contents, c.maxRows)
	if err != nil {
		return Rankings{}, fmt.Errorf("parse SwimCloud rankings: %w", err)
	}
	return Rankings{
		Men:       men,
		Women:     women,
		UpdatedAt: pythonISOTime(c.now()),
	}, nil
}

func pythonISOTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.999999-07:00")
}

type htmlNode struct {
	tag      string
	attrs    map[string]string
	text     string
	children []*htmlNode
}

func parseRankings(contents []byte, maxRows int) ([]Ranking, []Ranking, error) {
	root, err := parseHTML(contents)
	if err != nil {
		return nil, nil, err
	}
	section := firstDescendant(root, func(node *htmlNode) bool {
		return node.tag == "section" && node.attrs["id"] == "js-region-top-swims-container"
	})
	if section == nil {
		return []Ranking{}, []Ranking{}, nil
	}

	men := make([]Ranking, 0)
	women := make([]Ranking, 0)
	forms := descendants(section, func(node *htmlNode) bool {
		return node.tag == "div" && hasClass(node, "js-top-swims-form-content")
	})
	for _, form := range forms {
		for _, card := range form.children {
			if card.tag != "div" || !hasClass(card, "col-sm-6") {
				continue
			}
			genderElement := firstDescendant(card, func(node *htmlNode) bool {
				return node.tag == "h3" && hasClass(node, "c-title")
			})
			gender := ""
			if genderElement != nil {
				gender = nodeText(genderElement)
			}
			table := firstDescendant(card, func(node *htmlNode) bool {
				return node.tag == "table" && hasClass(node, "c-table-clean")
			})
			if table == nil {
				continue
			}
			rowRoot := firstDescendant(table, func(node *htmlNode) bool { return node.tag == "tbody" })
			if rowRoot == nil {
				rowRoot = table
			}
			rows := descendants(rowRoot, func(node *htmlNode) bool { return node.tag == "tr" })
			for index, row := range rows {
				position := index + 1
				if maxRows != 0 && position > maxRows {
					break
				}
				cells := descendants(row, func(node *htmlNode) bool { return node.tag == "td" })
				if len(cells) < 5 {
					continue
				}

				rank := nodeText(cells[0])
				if rank == "" {
					rank = fmt.Sprintf("%d", position)
				}
				nameElement := firstDescendant(cells[1], func(node *htmlNode) bool { return node.tag == "a" })
				name := nodeText(cells[1])
				if nameElement != nil {
					name = nodeText(nameElement)
				}
				if name == "" {
					continue
				}

				club := ""
				mobileClub := firstDescendant(cells[1], func(node *htmlNode) bool {
					return node.tag == "div" && hasClass(node, "u-color-mute")
				})
				if mobileClub != nil {
					club = nodeText(mobileClub)
				}
				if club == "" && len(cells) > 2 {
					teamLink := firstDescendant(cells[2], func(node *htmlNode) bool { return node.tag == "a" })
					if teamLink != nil {
						club = strings.TrimSpace(teamLink.attrs["title"])
					}
					if club == "" {
						teamImage := firstDescendant(cells[2], func(node *htmlNode) bool { return node.tag == "img" })
						if teamImage != nil {
							club = strings.TrimSpace(strings.ReplaceAll(teamImage.attrs["alt"], " logo", ""))
						}
					}
				}

				item := Ranking{
					Rank:  rank,
					Name:  name,
					Club:  club,
					Event: nodeText(cells[3]),
					Time:  nodeText(cells[4]),
				}
				if len(cells) > 5 {
					item.Score = nodeText(cells[5])
				}

				switch {
				case strings.HasPrefix(strings.ToLower(gender), "men"):
					men = append(men, item)
				case strings.HasPrefix(strings.ToLower(gender), "women"):
					women = append(women, item)
				}
			}
		}
	}
	return men, women, nil
}

func parseHTML(contents []byte) (*htmlNode, error) {
	root := &htmlNode{tag: "#document", attrs: map[string]string{}}
	stack := []*htmlNode{root}
	contents = scriptBlockPattern.ReplaceAll(contents, nil)
	contents = styleBlockPattern.ReplaceAll(contents, nil)
	decoder := xml.NewDecoder(strings.NewReader(string(contents)))
	decoder.Strict = false
	decoder.AutoClose = xml.HTMLAutoClose
	decoder.Entity = xml.HTMLEntity

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			node := &htmlNode{tag: strings.ToLower(value.Name.Local), attrs: make(map[string]string)}
			for _, attribute := range value.Attr {
				node.attrs[strings.ToLower(attribute.Name.Local)] = attribute.Value
			}
			parent := stack[len(stack)-1]
			parent.children = append(parent.children, node)
			stack = append(stack, node)
		case xml.EndElement:
			closing := strings.ToLower(value.Name.Local)
			for index := len(stack) - 1; index > 0; index-- {
				if stack[index].tag == closing {
					stack = stack[:index]
					break
				}
			}
		case xml.CharData:
			stack[len(stack)-1].text += string(value)
		}
	}
	return root, nil
}

func hasClass(node *htmlNode, className string) bool {
	for _, configured := range strings.Fields(node.attrs["class"]) {
		if configured == className {
			return true
		}
	}
	return false
}

func firstDescendant(node *htmlNode, match func(*htmlNode) bool) *htmlNode {
	for _, child := range node.children {
		if match(child) {
			return child
		}
		if found := firstDescendant(child, match); found != nil {
			return found
		}
	}
	return nil
}

func descendants(node *htmlNode, match func(*htmlNode) bool) []*htmlNode {
	found := make([]*htmlNode, 0)
	for _, child := range node.children {
		if match(child) {
			found = append(found, child)
		}
		found = append(found, descendants(child, match)...)
	}
	return found
}

func nodeText(node *htmlNode) string {
	var text strings.Builder
	var appendText func(*htmlNode)
	appendText = func(current *htmlNode) {
		text.WriteString(strings.TrimSpace(current.text))
		for _, child := range current.children {
			appendText(child)
		}
	}
	appendText(node)
	return strings.TrimSpace(text.String())
}
