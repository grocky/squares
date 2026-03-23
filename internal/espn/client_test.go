package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Pure function tests ---

func TestNormalizeStatus(t *testing.T) {
	tests := []struct {
		state string
		want  string
	}{
		{"post", "final"},
		{"in", "in_progress"},
		{"pre", "scheduled"},
		{"", "scheduled"},
		{"unknown", "scheduled"},
	}
	for _, tc := range tests {
		got := normalizeStatus(tc.state)
		if got != tc.want {
			t.Errorf("normalizeStatus(%q) = %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestRoundFromHeadline(t *testing.T) {
	tests := []struct {
		headline string
		want     int
	}{
		{"NCAA Men's Basketball Championship - East Region - 1st Round", 1},
		{"NCAA Men's Basketball Championship - West Region - First Round", 1},
		{"NCAA Men's Basketball Championship - East Region - 2nd Round", 2},
		{"NCAA Men's Basketball Championship - South Region - Second Round", 2},
		{"NCAA Men's Basketball Championship - Sweet 16", 3},
		{"NCAA Men's Basketball Championship - Sweet Sixteen", 3},
		{"NCAA Men's Basketball Championship - Elite 8", 4},
		{"NCAA Men's Basketball Championship - Elite Eight", 4},
		{"NCAA Men's Basketball Championship - Final Four", 5},
		{"NCAA Men's Basketball Championship - National Championship", 6},
		{"NCAA Men's Basketball Championship - Championship Game", 6},
		// Unknown headline returns 0, not 1
		{"Some Random Headline", 0},
		{"", 0},
		{"NCAA Men's Basketball Championship - Quarterfinals", 0},
	}
	for _, tc := range tests {
		got := roundFromHeadline(tc.headline)
		if got != tc.want {
			t.Errorf("roundFromHeadline(%q) = %d, want %d", tc.headline, got, tc.want)
		}
	}
}

func TestRoundFromHeadline_CaseInsensitive(t *testing.T) {
	got := roundFromHeadline("FINAL FOUR")
	if got != 5 {
		t.Errorf("roundFromHeadline(\"FINAL FOUR\") = %d, want 5", got)
	}
}

func TestDateRange_ValidRange(t *testing.T) {
	// Use a date far in the past so today is always after it
	dates, err := dateRange("20200101")
	if err != nil {
		t.Fatalf("dateRange returned error: %v", err)
	}
	if len(dates) == 0 {
		t.Fatal("dateRange returned empty slice")
	}
	if dates[0] != "20200101" {
		t.Errorf("first date = %q, want %q", dates[0], "20200101")
	}
	// All dates should be in YYYYMMDD format
	for _, d := range dates {
		if len(d) != 8 {
			t.Errorf("date %q is not YYYYMMDD format", d)
		}
	}
}

func TestDateRange_InvalidDate(t *testing.T) {
	_, err := dateRange("not-a-date")
	if err == nil {
		t.Error("dateRange(\"not-a-date\") should return error")
	}
}

func TestDateRange_FutureDate(t *testing.T) {
	dates, err := dateRange("20990101")
	if err != nil {
		t.Fatalf("dateRange returned error: %v", err)
	}
	if len(dates) != 0 {
		t.Errorf("dateRange with future start should return empty, got %d dates", len(dates))
	}
}

// --- FetchGames with httptest mock ---

func TestFetchGames_ParsesEventsCorrectly(t *testing.T) {
	resp := scoreboardResponse{
		Events: []event{
			{
				ID:   "401234",
				Name: "Duke vs UNC",
				Date: "2026-03-19T19:00Z",
				Competitions: []competition{
					{
						Competitors: []competitor{
							newCompetitor("home", "75", "Duke", 1),
							newCompetitor("away", "68", "UNC", 2),
						},
						Status: struct {
							Type struct {
								Name  string `json:"name"`
								State string `json:"state"`
							} `json:"type"`
						}{Type: struct {
							Name  string `json:"name"`
							State string `json:"state"`
						}{Name: "STATUS_FINAL", State: "post"}},
						Notes: []struct {
							Headline string `json:"headline"`
						}{{Headline: "NCAA Men's Basketball Championship - East Region - 1st Round"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{
		httpClient: srv.Client(),
	}

	// Call fetchDate directly against our test server
	events, err := c.fetchDateURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchDate returned error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].ID != "401234" {
		t.Errorf("event ID = %q, want %q", events[0].ID, "401234")
	}
}

func TestFetchGames_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &Client{
		httpClient: srv.Client(),
	}

	_, err := c.fetchDateURL(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected error on 500 response")
	}
}

func TestFetchGames_EmptyCompetitions(t *testing.T) {
	// Events with no competitions should be skipped in FetchGames
	resp := scoreboardResponse{
		Events: []event{
			{
				ID:           "401234",
				Date:         "2026-03-19T19:00Z",
				Competitions: []competition{},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{
		httpClient: srv.Client(),
	}

	events, err := c.fetchDateURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchDate returned error: %v", err)
	}
	// The event is returned from fetchDate; it's FetchGames that filters
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func TestFetchGames_DeduplicatesByID(t *testing.T) {
	// Two dates return the same event ID; later should win
	earlyResp := scoreboardResponse{
		Events: []event{
			{
				ID:   "401234",
				Name: "Duke vs UNC (early)",
				Date: "2026-03-19T19:00Z",
				Competitions: []competition{
					{
						Competitors: []competitor{
							newCompetitor("home", "0", "Duke", 1),
							newCompetitor("away", "0", "UNC", 2),
						},
						Status: struct {
							Type struct {
								Name  string `json:"name"`
								State string `json:"state"`
							} `json:"type"`
						}{Type: struct {
							Name  string `json:"name"`
							State string `json:"state"`
						}{State: "pre"}},
					},
				},
			},
		},
	}

	lateResp := scoreboardResponse{
		Events: []event{
			{
				ID:   "401234",
				Name: "Duke vs UNC (late)",
				Date: "2026-03-19T19:00Z",
				Competitions: []competition{
					{
						Competitors: []competitor{
							newCompetitor("home", "75", "Duke", 1),
							newCompetitor("away", "68", "UNC", 2),
						},
						Status: struct {
							Type struct {
								Name  string `json:"name"`
								State string `json:"state"`
							} `json:"type"`
						}{Type: struct {
							Name  string `json:"name"`
							State string `json:"state"`
						}{State: "post"}},
					},
				},
			},
		},
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			json.NewEncoder(w).Encode(earlyResp)
		} else {
			json.NewEncoder(w).Encode(lateResp)
		}
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client()}

	// Simulate what FetchGames does: fetch two dates and deduplicate
	seen := make(map[string]event)
	for i := 0; i < 2; i++ {
		events, err := c.fetchDateURL(context.Background(), srv.URL)
		if err != nil {
			t.Fatalf("fetchDate returned error: %v", err)
		}
		for _, ev := range events {
			seen[ev.ID] = ev
		}
	}
	if len(seen) != 1 {
		t.Errorf("got %d unique events, want 1", len(seen))
	}
	ev := seen["401234"]
	if ev.Name != "Duke vs UNC (late)" {
		t.Errorf("dedup should keep later version, got Name=%q", ev.Name)
	}
}

func TestFetchGames_ScoreParsing(t *testing.T) {
	// Verify WinnerScore/LoserScore derivation: home wins
	resp := scoreboardResponse{
		Events: []event{
			{
				ID:   "401",
				Date: time.Now().UTC().Format("20060102"),
				Competitions: []competition{
					{
						Competitors: []competitor{
							newCompetitor("home", "80", "A", 1),
							newCompetitor("away", "65", "B", 2),
						},
						Status: struct {
							Type struct {
								Name  string `json:"name"`
								State string `json:"state"`
							} `json:"type"`
						}{Type: struct {
							Name  string `json:"name"`
							State string `json:"state"`
						}{State: "post"}},
						Notes: []struct {
							Headline string `json:"headline"`
						}{{Headline: "1st Round"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client()}

	games, err := c.fetchGamesFromURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchGamesFromURL error: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	g := games[0]
	if g.HomeTeam != "A" || g.AwayTeam != "B" {
		t.Errorf("teams = %q/%q, want A/B", g.HomeTeam, g.AwayTeam)
	}
	if g.HomeScore != 80 || g.AwayScore != 65 {
		t.Errorf("scores = %d/%d, want 80/65", g.HomeScore, g.AwayScore)
	}
	if g.HomeRank != 1 || g.AwayRank != 2 {
		t.Errorf("ranks = %d/%d, want 1/2", g.HomeRank, g.AwayRank)
	}
	if g.WinnerScore != 80 || g.LoserScore != 65 {
		t.Errorf("winner/loser = %d/%d, want 80/65", g.WinnerScore, g.LoserScore)
	}
	if g.Status != "final" {
		t.Errorf("status = %q, want %q", g.Status, "final")
	}
	if g.RoundNum != 1 {
		t.Errorf("roundNum = %d, want 1", g.RoundNum)
	}
}

func TestFetchGames_AwayTeamWins(t *testing.T) {
	resp := scoreboardResponse{
		Events: []event{
			{
				ID:   "402",
				Date: time.Now().UTC().Format("20060102"),
				Competitions: []competition{
					{
						Competitors: []competitor{
							newCompetitor("home", "55", "A", 1),
							newCompetitor("away", "70", "B", 2),
						},
						Status: struct {
							Type struct {
								Name  string `json:"name"`
								State string `json:"state"`
							} `json:"type"`
						}{Type: struct {
							Name  string `json:"name"`
							State string `json:"state"`
						}{State: "post"}},
					},
				},
			},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := &Client{httpClient: srv.Client()}

	games, err := c.fetchGamesFromURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	g := games[0]
	if g.WinnerScore != 70 || g.LoserScore != 55 {
		t.Errorf("winner/loser = %d/%d, want 70/55", g.WinnerScore, g.LoserScore)
	}
}

func newCompetitor(homeAway, score, teamName string, rank int) competitor {
	return competitor{
		HomeAway: homeAway,
		Score:    score,
		Team: struct {
			DisplayName string `json:"displayName"`
		}{DisplayName: teamName},
		CuratedRank: struct {
			Current int `json:"current"`
		}{Current: rank},
	}
}
