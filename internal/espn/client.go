package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/models"
)

const scoreboardURL = "https://site.api.espn.com/apis/site/v2/sports/basketball/mens-college-basketball/scoreboard?groups=100&limit=50"

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
	Competitions []competition `json:"competitions"`
	Season       struct {
		Slug string `json:"slug"`
	} `json:"season"`
}

type competition struct {
	Competitors []competitor `json:"competitors"`
	Status      struct {
		Type struct {
			Name string `json:"name"`
		} `json:"type"`
	} `json:"status"`
}

type competitor struct {
	HomeAway string `json:"homeAway"`
	Score    string `json:"score"`
	Team     struct {
		DisplayName string `json:"displayName"`
	} `json:"team"`
}

func normalizeStatus(espnStatus string) string {
	switch espnStatus {
	case "STATUS_FINAL":
		return "final"
	case "STATUS_IN_PROGRESS":
		return "in_progress"
	default:
		return "scheduled"
	}
}

func (c *Client) FetchGames(ctx context.Context) ([]models.Game, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scoreboardURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching scoreboard: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ESPN API returned status %d", resp.StatusCode)
	}

	var sb scoreboardResponse
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	var games []models.Game
	for _, ev := range sb.Events {
		if len(ev.Competitions) == 0 {
			continue
		}
		comp := ev.Competitions[0]
		g := models.Game{
			EspnID:   ev.ID,
			Status:   normalizeStatus(comp.Status.Type.Name),
			SyncedAt: time.Now().UTC(),
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

		// Determine winner/loser scores: higher score = winner
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
	games, err := c.FetchGames(ctx)
	if err != nil {
		return nil, err
	}
	for i := range games {
		games[i].PoolID = poolID
		if err := c.repo.PutGame(ctx, games[i]); err != nil {
			return nil, fmt.Errorf("upserting game %s: %w", games[i].EspnID, err)
		}
	}
	return games, nil
}
