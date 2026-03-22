package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/models"
)

const scoreboardBaseURL = "https://site.api.espn.com/apis/site/v2/sports/basketball/mens-college-basketball/scoreboard?groups=100&limit=50"

// tournamentStartDate is the first day of the NCAA tournament (Round of 64).
// Update this each year.
const tournamentStartDate = "20260319"

type Client struct {
	httpClient *http.Client
	repo       *dynamo.Repo
}

func NewClient(repo *dynamo.Repo) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		repo:       repo,
	}
}

type scoreboardResponse struct {
	Events []event `json:"events"`
}

type event struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Date         string        `json:"date"`
	Competitions []competition `json:"competitions"`
}

type competition struct {
	Competitors []competitor `json:"competitors"`
	Status      struct {
		Type struct {
			Name  string `json:"name"`
			State string `json:"state"` // "pre", "in", "post"
		} `json:"type"`
	} `json:"status"`
	Notes []struct {
		Headline string `json:"headline"`
	} `json:"notes"`
}

type competitor struct {
	HomeAway string `json:"homeAway"`
	Score    string `json:"score"`
	Team     struct {
		DisplayName string `json:"displayName"`
	} `json:"team"`
}

// normalizeStatus uses the ESPN state field which is more stable than name.
// "pre" → scheduled, "in" → in_progress, "post" → final
func normalizeStatus(state string) string {
	switch state {
	case "post":
		return "final"
	case "in":
		return "in_progress"
	default:
		return "scheduled"
	}
}

// roundFromHeadline parses "NCAA Men's Basketball Championship - Region - Nth Round"
// or "NCAA Men's Basketball Championship - Final Four" etc.
func roundFromHeadline(headline string) int {
	h := strings.ToLower(headline)
	switch {
	case strings.Contains(h, "1st round") || strings.Contains(h, "first round"):
		return 1
	case strings.Contains(h, "2nd round") || strings.Contains(h, "second round"):
		return 2
	case strings.Contains(h, "sweet 16") || strings.Contains(h, "sweet sixteen"):
		return 3
	case strings.Contains(h, "elite 8") || strings.Contains(h, "elite eight"):
		return 4
	case strings.Contains(h, "final four"):
		return 5
	case strings.Contains(h, "national championship") || strings.Contains(h, "championship game"):
		return 6
	default:
		return 0 // unknown; caller should preserve existing round number
	}
}

// fetchDate fetches games for a single YYYYMMDD date string.
func (c *Client) fetchDate(ctx context.Context, date string) ([]event, error) {
	url := scoreboardBaseURL + "&dates=" + date
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching scoreboard for %s: %w", date, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN API returned status %d for date %s", resp.StatusCode, date)
	}

	var sb scoreboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decoding response for %s: %w", date, err)
	}
	return sb.Events, nil
}

// dateRange returns all YYYYMMDD strings from start through today (inclusive).
func dateRange(startDate string) ([]string, error) {
	start, err := time.Parse("20060102", startDate)
	if err != nil {
		return nil, fmt.Errorf("invalid start date %q: %w", startDate, err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var dates []string
	for d := start; !d.After(today); d = d.AddDate(0, 0, 1) {
		dates = append(dates, d.Format("20060102"))
	}
	return dates, nil
}

func (c *Client) FetchGames(ctx context.Context) ([]models.Game, error) {
	dates, err := dateRange(tournamentStartDate)
	if err != nil {
		return nil, err
	}

	// Deduplicate by ESPN ID — later dates win (more up-to-date status)
	seen := make(map[string]event)
	for _, date := range dates {
		events, err := c.fetchDate(ctx, date)
		if err != nil {
			return nil, err
		}
		for _, ev := range events {
			seen[ev.ID] = ev
		}
	}

	var allEvents []event
	for _, ev := range seen {
		allEvents = append(allEvents, ev)
	}

	var games []models.Game
	for _, ev := range allEvents {
		if len(ev.Competitions) == 0 {
			continue
		}
		comp := ev.Competitions[0]

		status := normalizeStatus(comp.Status.Type.State)

		// Parse round from notes headline; 0 means unknown
		roundNum := 0
		if len(comp.Notes) > 0 {
			roundNum = roundFromHeadline(comp.Notes[0].Headline)
		}

		startTime, _ := time.Parse("2006-01-02T15:04Z", ev.Date)
		if startTime.IsZero() {
			startTime, _ = time.Parse(time.RFC3339, ev.Date)
		}

		g := models.Game{
			EspnID:    ev.ID,
			Status:    status,
			RoundNum:  roundNum,
			StartTime: startTime,
			SyncedAt:  time.Now().UTC(),
		}

		var homeTeam, awayTeam string
		var homeScore, awayScore int
		for _, c := range comp.Competitors {
			score, _ := strconv.Atoi(c.Score)
			if c.HomeAway == "home" {
				homeTeam = c.Team.DisplayName
				homeScore = score
			} else {
				awayTeam = c.Team.DisplayName
				awayScore = score
			}
		}

		g.HomeTeam = homeTeam
		g.AwayTeam = awayTeam
		g.HomeScore = homeScore
		g.AwayScore = awayScore

		// Derive winner/loser scores for the scorer logic
		if homeScore >= awayScore {
			g.WinnerScore = homeScore
			g.LoserScore = awayScore
		} else {
			g.WinnerScore = awayScore
			g.LoserScore = homeScore
		}

		games = append(games, g)
	}
	return games, nil
}

func (c *Client) SyncGames(ctx context.Context, poolID string) ([]models.Game, error) {
	freshGames, err := c.FetchGames(ctx)
	if err != nil {
		return nil, err
	}

	// Load existing games so we never regress a status
	existingGames, _ := c.repo.GetAllGames(ctx, poolID)
	existingMap := make(map[string]models.Game)
	for _, g := range existingGames {
		existingMap[g.EspnID] = g
	}

	statusRank := map[string]int{"scheduled": 0, "in_progress": 1, "final": 2}

	for i := range freshGames {
		g := &freshGames[i]
		g.PoolID = poolID

		// Never regress status (e.g. final → in_progress or scheduled)
		if existing, ok := existingMap[g.EspnID]; ok {
			if statusRank[existing.Status] > statusRank[g.Status] {
				g.Status = existing.Status
			}
			// Preserve round number if ESPN stops returning it
			if g.RoundNum == 0 && existing.RoundNum > 0 {
				g.RoundNum = existing.RoundNum
			}
			// Preserve start time if we failed to parse it this sync
			if g.StartTime.IsZero() && !existing.StartTime.IsZero() {
				g.StartTime = existing.StartTime
			}
		}

		if err := c.repo.PutGame(ctx, *g); err != nil {
			return nil, fmt.Errorf("upserting game %s: %w", g.EspnID, err)
		}
	}
	return freshGames, nil
}
