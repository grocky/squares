package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/grocky/squares/internal/models"
)

func TestParseRoundFilter(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"?round=1", 1},
		{"?round=3", 3},
		{"?round=6", 6},
		{"?round=0", 0},  // out of range
		{"?round=7", 0},  // out of range
		{"?round=-1", 0}, // negative
		{"?round=abc", 0},
		{"", 0},           // no param
		{"?other=2", 0},   // wrong param name
	}
	for _, tc := range tests {
		r, _ := http.NewRequest("GET", "/pools/main/grid"+tc.query, nil)
		got := parseRoundFilter(r)
		if got != tc.want {
			t.Errorf("parseRoundFilter(%q) = %d, want %d", tc.query, got, tc.want)
		}
	}
}

func TestCurrentRound_InProgressPreferred(t *testing.T) {
	games := []models.Game{
		{RoundNum: 1, Status: "final"},
		{RoundNum: 2, Status: "in_progress"},
		{RoundNum: 3, Status: "scheduled"},
	}
	got := currentRound(games)
	if got != 2 {
		t.Errorf("currentRound = %d, want 2 (highest in_progress)", got)
	}
}

func TestCurrentRound_TodayGames(t *testing.T) {
	now := time.Now().UTC()
	games := []models.Game{
		{RoundNum: 1, Status: "final", StartTime: now.AddDate(0, 0, -2)},
		{RoundNum: 3, Status: "scheduled", StartTime: now},
	}
	got := currentRound(games)
	if got != 3 {
		t.Errorf("currentRound = %d, want 3 (today's game)", got)
	}
}

func TestCurrentRound_FallbackToHighestFinal(t *testing.T) {
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	games := []models.Game{
		{RoundNum: 1, Status: "final", StartTime: yesterday},
		{RoundNum: 3, Status: "final", StartTime: yesterday},
		{RoundNum: 2, Status: "final", StartTime: yesterday},
	}
	got := currentRound(games)
	if got != 3 {
		t.Errorf("currentRound = %d, want 3 (highest final)", got)
	}
}

func TestCurrentRound_EmptyGames(t *testing.T) {
	got := currentRound(nil)
	if got != 1 {
		t.Errorf("currentRound(nil) = %d, want 1 (default)", got)
	}
}

func TestCurrentRound_AllScheduled(t *testing.T) {
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	games := []models.Game{
		{RoundNum: 2, Status: "scheduled", StartTime: tomorrow},
		{RoundNum: 3, Status: "scheduled", StartTime: tomorrow},
	}
	got := currentRound(games)
	// No in_progress, no today games, no final games → falls back to latest=1
	if got != 1 {
		t.Errorf("currentRound = %d, want 1 (fallback)", got)
	}
}

func TestCurrentRound_MultipleInProgress(t *testing.T) {
	games := []models.Game{
		{RoundNum: 1, Status: "in_progress"},
		{RoundNum: 3, Status: "in_progress"},
		{RoundNum: 2, Status: "in_progress"},
	}
	got := currentRound(games)
	if got != 3 {
		t.Errorf("currentRound = %d, want 3 (highest in_progress)", got)
	}
}
