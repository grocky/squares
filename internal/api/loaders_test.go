package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grocky/squares/internal/models"
)

// mockRepo tracks which methods are called to verify each loader's query budget.
type mockRepo struct {
	calls map[string]int

	pool         models.Pool
	roundConfigs []models.RoundConfig
	axes         []models.Axis
	squares      []models.Square
	games        []models.Game
	payouts      []models.Payout
}

func newMockRepo() *mockRepo {
	return &mockRepo{calls: make(map[string]int)}
}

func (m *mockRepo) GetPool(_ context.Context, poolID string) (models.Pool, error) {
	m.calls["GetPool"]++
	return m.pool, nil
}
func (m *mockRepo) PutPool(_ context.Context, _ models.Pool) error {
	m.calls["PutPool"]++
	return nil
}
func (m *mockRepo) GetAllRoundConfigs(_ context.Context, _ string) ([]models.RoundConfig, error) {
	m.calls["GetAllRoundConfigs"]++
	return m.roundConfigs, nil
}
func (m *mockRepo) GetRoundConfig(_ context.Context, _ string, roundNum int) (models.RoundConfig, error) {
	m.calls["GetRoundConfig"]++
	for _, rc := range m.roundConfigs {
		if rc.RoundNum == roundNum {
			return rc, nil
		}
	}
	return models.RoundConfig{}, fmt.Errorf("not found")
}
func (m *mockRepo) PutRoundConfig(_ context.Context, _ models.RoundConfig) error {
	m.calls["PutRoundConfig"]++
	return nil
}
func (m *mockRepo) GetAllRoundAxes(_ context.Context, _ string) ([]models.Axis, error) {
	m.calls["GetAllRoundAxes"]++
	return m.axes, nil
}
func (m *mockRepo) GetRoundAxis(_ context.Context, _ string, roundNum int, axisType string) (models.Axis, error) {
	m.calls["GetRoundAxis"]++
	for _, ax := range m.axes {
		if ax.RoundNum == roundNum && ax.Type == axisType {
			return ax, nil
		}
	}
	return models.Axis{}, fmt.Errorf("not found")
}
func (m *mockRepo) PutRoundAxis(_ context.Context, _ models.Axis) error {
	m.calls["PutRoundAxis"]++
	return nil
}
func (m *mockRepo) GetAllSquares(_ context.Context, _ string) ([]models.Square, error) {
	m.calls["GetAllSquares"]++
	return m.squares, nil
}
func (m *mockRepo) PutSquare(_ context.Context, _ models.Square) error {
	m.calls["PutSquare"]++
	return nil
}
func (m *mockRepo) GetAllGamesGlobal(_ context.Context) ([]models.Game, error) {
	m.calls["GetAllGamesGlobal"]++
	return m.games, nil
}
func (m *mockRepo) GetAllPayouts(_ context.Context, _ string) ([]models.Payout, error) {
	m.calls["GetAllPayouts"]++
	return m.payouts, nil
}

func (m *mockRepo) called(method string) int {
	return m.calls[method]
}

func newTestHandler(repo *mockRepo) *Handler {
	return &Handler{repo: repo, cache: newPoolCache(60 * time.Second)}
}

func TestLoadFullDashboard_CallsAllRepoMethods(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1", Name: "Test"}
	h := newTestHandler(repo)

	data, err := h.loadFullDashboard(context.Background(), "p1", 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if data.Pool.ID != "p1" {
		t.Errorf("Pool.ID = %q, want p1", data.Pool.ID)
	}

	for _, method := range []string{"GetPool", "GetAllRoundConfigs", "GetAllRoundAxes", "GetAllSquares", "GetAllGamesGlobal", "GetAllPayouts"} {
		if repo.called(method) != 1 {
			t.Errorf("%s called %d times, want 1", method, repo.called(method))
		}
	}
}

func TestLoadGridData_CallsExpectedMethods(t *testing.T) {
	repo := newMockRepo()
	h := newTestHandler(repo)

	_, err := h.loadGridData(context.Background(), "p1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// loadGridData uses loadPoolMetadata (with cache), which fetches pool on cache miss
	for _, method := range []string{"GetPool", "GetAllRoundConfigs", "GetAllRoundAxes", "GetAllSquares", "GetAllGamesGlobal", "GetAllPayouts"} {
		if repo.called(method) != 1 {
			t.Errorf("%s called %d times, want 1", method, repo.called(method))
		}
	}
}

func TestLoadLeaderboardData_DoesNotCallSquaresOrAxes(t *testing.T) {
	repo := newMockRepo()
	repo.payouts = []models.Payout{
		{OwnerName: "Alice", Amount: 10, GameID: "g1"},
		{OwnerName: "Alice", Amount: 20, GameID: "g2"},
		{OwnerName: "Bob", Amount: 15, GameID: "g3"},
	}
	repo.games = []models.Game{
		{EspnID: "g1", RoundNum: 1},
		{EspnID: "g2", RoundNum: 2},
		{EspnID: "g3", RoundNum: 1},
	}
	repo.roundConfigs = []models.RoundConfig{
		{RoundNum: 1, PayoutAmount: 10},
		{RoundNum: 2, PayoutAmount: 20},
	}
	h := newTestHandler(repo)

	data, err := h.loadLeaderboardData(context.Background(), "p1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must NOT call GetAllSquares or GetAllRoundAxes
	if repo.called("GetAllSquares") != 0 {
		t.Errorf("GetAllSquares should not be called by loadLeaderboardData, called %d times", repo.called("GetAllSquares"))
	}
	if repo.called("GetAllRoundAxes") != 0 {
		t.Errorf("GetAllRoundAxes should not be called by loadLeaderboardData, called %d times", repo.called("GetAllRoundAxes"))
	}

	// Should call payouts and games (for amount override)
	if repo.called("GetAllPayouts") != 1 {
		t.Errorf("GetAllPayouts called %d times, want 1", repo.called("GetAllPayouts"))
	}
	if repo.called("GetAllGamesGlobal") != 1 {
		t.Errorf("GetAllGamesGlobal called %d times, want 1", repo.called("GetAllGamesGlobal"))
	}

	// Verify leaderboard content
	if len(data.Leaderboard) != 2 {
		t.Fatalf("Leaderboard has %d entries, want 2", len(data.Leaderboard))
	}
	// Alice should be first (total 30 > 10)
	if data.Leaderboard[0].Name != "Alice" {
		t.Errorf("Leaderboard[0].Name = %q, want Alice", data.Leaderboard[0].Name)
	}
	if data.Leaderboard[0].Total != 30 {
		t.Errorf("Leaderboard[0].Total = %f, want 30", data.Leaderboard[0].Total)
	}
	if data.Leaderboard[0].Wins != 2 {
		t.Errorf("Leaderboard[0].Wins = %d, want 2", data.Leaderboard[0].Wins)
	}
}

func TestLoadGamesData_DoesNotCallPayoutsOrSquares(t *testing.T) {
	repo := newMockRepo()
	repo.games = []models.Game{
		{EspnID: "g1", RoundNum: 1, Status: "final"},
		{EspnID: "g2", RoundNum: 2, Status: "scheduled"},
		{EspnID: "g3", RoundNum: 1, Status: "in_progress"},
	}
	h := newTestHandler(repo)

	data, err := h.loadGamesData(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must NOT call GetAllPayouts or GetAllSquares
	if repo.called("GetAllPayouts") != 0 {
		t.Errorf("GetAllPayouts should not be called by loadGamesData, called %d times", repo.called("GetAllPayouts"))
	}
	if repo.called("GetAllSquares") != 0 {
		t.Errorf("GetAllSquares should not be called by loadGamesData, called %d times", repo.called("GetAllSquares"))
	}

	// Should only call GetAllGamesGlobal
	if repo.called("GetAllGamesGlobal") != 1 {
		t.Errorf("GetAllGamesGlobal called %d times, want 1", repo.called("GetAllGamesGlobal"))
	}

	// Round filter should return only round 1 games
	if len(data.Games) != 2 {
		t.Fatalf("Games has %d entries, want 2 (round 1 only)", len(data.Games))
	}
	for _, g := range data.Games {
		if g.RoundNum != 1 {
			t.Errorf("game %s has RoundNum=%d, want 1", g.EspnID, g.RoundNum)
		}
	}
}

func TestLoadGamesData_NoFilter_ReturnsAll(t *testing.T) {
	repo := newMockRepo()
	repo.games = []models.Game{
		{EspnID: "g1", RoundNum: 1},
		{EspnID: "g2", RoundNum: 2},
	}
	h := newTestHandler(repo)

	data, err := h.loadGamesData(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data.Games) != 2 {
		t.Errorf("Games has %d entries, want 2", len(data.Games))
	}
}

func TestLoadGridData_PayoutWinnerOverlay(t *testing.T) {
	repo := newMockRepo()
	repo.squares = []models.Square{{Row: 3, Col: 5, OwnerName: "Alice"}}
	repo.payouts = []models.Payout{{Row: 3, Col: 5, Amount: 10, GameID: "g1", OwnerName: "Alice"}}
	repo.games = []models.Game{{EspnID: "g1", RoundNum: 1}}
	repo.roundConfigs = []models.RoundConfig{{RoundNum: 1, PayoutAmount: 10}}
	repo.axes = []models.Axis{
		{RoundNum: 1, Type: "winner", Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{RoundNum: 1, Type: "loser", Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
	}
	h := newTestHandler(repo)

	data, err := h.loadGridData(context.Background(), "p1", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cell := data.Grid[3][5]
	if !cell.IsWinner {
		t.Error("Grid[3][5].IsWinner should be true")
	}
	if cell.Amount != 10 {
		t.Errorf("Grid[3][5].Amount = %f, want 10", cell.Amount)
	}
}

func TestLoadFullDashboard_RoundFilter(t *testing.T) {
	repo := newMockRepo()
	repo.pool = models.Pool{ID: "p1"}
	repo.games = []models.Game{
		{EspnID: "g1", RoundNum: 1},
		{EspnID: "g2", RoundNum: 2},
	}
	repo.payouts = []models.Payout{
		{GameID: "g1", Row: 0, Col: 0, Amount: 5, OwnerName: "Alice"},
		{GameID: "g2", Row: 1, Col: 1, Amount: 10, OwnerName: "Bob"},
	}
	h := newTestHandler(repo)

	data, err := h.loadFullDashboard(context.Background(), "p1", 1, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only round 1 games
	if len(data.Games) != 1 {
		t.Errorf("Games = %d, want 1 (round 1 only)", len(data.Games))
	}
	// Filtered payouts: only round 1
	if len(data.Payouts) != 1 {
		t.Errorf("Payouts = %d, want 1 (round 1 only)", len(data.Payouts))
	}
	// Leaderboard uses ALL payouts (both rounds)
	if len(data.Leaderboard) != 2 {
		t.Errorf("Leaderboard = %d, want 2 (all rounds)", len(data.Leaderboard))
	}
}
