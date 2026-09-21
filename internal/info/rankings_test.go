package info

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const rankingFixture = `<!doctype html>
<html><body>
<section id="js-region-top-swims-container">
  <div class="js-top-swims-form-content">
    <div class="col-sm-6">
      <h3 class="c-title">Men - Top Swims</h3>
      <table class="c-table-clean"><tbody>
        <tr>
          <td></td>
          <td><a>Luka Mijatovic</a><div class="u-color-mute">Pleasanton Seahawks</div></td>
          <td><a title="Ignored Team"></a></td>
          <td>400 Free</td><td>3:45.30</td><td>932</td>
        </tr>
        <tr>
          <td>2</td><td><a>Second Swimmer</a></td>
          <td><a title="Second Club"></a></td>
          <td>50 Back</td><td>24.00</td><td>900</td>
        </tr>
      </tbody></table>
    </div>
    <div class="col-sm-6">
      <h3 class="c-title">Women</h3>
      <table class="c-table-clean"><tbody>
        <tr>
          <td>1</td><td><a>Woman Swimmer</a></td>
          <td><img alt="Women Club logo"></td>
          <td>100 Fly</td><td>55.50</td><td>910</td>
        </tr>
      </tbody></table>
    </div>
  </div>
</section>
</body></html>`

func TestRankingsClientBuildsSwimCloudRequestAndParsesFixture(t *testing.T) {
	var receivedRegion string
	var receivedUserAgent string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedRegion = r.URL.Query().Get("r")
		receivedUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(rankingFixture))
	}))
	t.Cleanup(upstream.Close)

	client := NewRankingsClient(upstream.Client(), upstream.URL+"/top-swims?existing=kept")
	client.now = func() time.Time {
		return time.Date(2030, time.March, 4, 5, 6, 7, 123456000, time.UTC)
	}
	rankings, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if receivedRegion != "country_USA" || receivedUserAgent != rankingUserAgent {
		t.Fatalf("request region/User-Agent = %q/%q", receivedRegion, receivedUserAgent)
	}
	if len(rankings.Men) != 2 || len(rankings.Women) != 1 {
		t.Fatalf("ranking sizes = men %d women %d", len(rankings.Men), len(rankings.Women))
	}
	wantFirst := Ranking{Rank: "1", Name: "Luka Mijatovic", Club: "Pleasanton Seahawks", Event: "400 Free", Time: "3:45.30", Score: "932"}
	if rankings.Men[0] != wantFirst {
		t.Fatalf("first men's row = %+v, want %+v", rankings.Men[0], wantFirst)
	}
	if rankings.Men[1].Club != "Second Club" || rankings.Women[0].Club != "Women Club" {
		t.Fatalf("fallback clubs = %q/%q", rankings.Men[1].Club, rankings.Women[0].Club)
	}
	if rankings.UpdatedAt != "2030-03-04T05:06:07.123456+00:00" {
		t.Fatalf("updated_at = %q", rankings.UpdatedAt)
	}
}

func TestRankingsParserHandlesMissingOrMalformedExpectedMarkup(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{name: "missing section", html: `<html><body><p>No rankings</p></body></html>`},
		{name: "unsafe script is ignored", html: `<html><script>if (value < other) { document.write("<b>"); }</script><body><p>No rankings</p></body></html>`},
		{name: "short malformed row", html: `<section id="js-region-top-swims-container"><div class="js-top-swims-form-content"><div class="col-sm-6"><h3 class="c-title">Men</h3><table class="c-table-clean"><tr><td>1</td><td>Name</td></tr></table></div></div></section>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			men, women, err := parseRankings([]byte(test.html), defaultRankingRows)
			if err != nil {
				t.Fatalf("parseRankings() error = %v", err)
			}
			if len(men) != 0 || len(women) != 0 {
				t.Fatalf("rankings = men %+v women %+v, want empty", men, women)
			}
		})
	}
}

func TestRankingsClientLimitsRowsLikeFlask(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(rankingFixture))
	}))
	t.Cleanup(upstream.Close)
	client := NewRankingsClient(upstream.Client(), upstream.URL)
	client.maxRows = 1

	rankings, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(rankings.Men) != 1 || len(rankings.Women) != 1 {
		t.Fatalf("limited rankings = men %d women %d, want 1/1", len(rankings.Men), len(rankings.Women))
	}
}

func TestRankingsClientReturnsErrorsForUpstreamFailureAndTimeout(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "private upstream detail", http.StatusBadGateway)
		}))
		t.Cleanup(upstream.Close)
		_, err := NewRankingsClient(upstream.Client(), upstream.URL).Fetch(context.Background())
		if err == nil {
			t.Fatal("Fetch() error = nil, want upstream failure")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(rankingFixture))
		}))
		t.Cleanup(upstream.Close)
		httpClient := upstream.Client()
		httpClient.Timeout = 10 * time.Millisecond
		_, err := NewRankingsClient(httpClient, upstream.URL).Fetch(context.Background())
		if err == nil {
			t.Fatal("Fetch() error = nil, want timeout")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout error = %v, want context deadline", err)
		}
	})
}
