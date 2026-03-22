package syncer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grocky/squares/internal/models"
)

// --- Mocks ---

type mockESPN struct {
	games []models.Game
	err   error
}

func (m *mockESPN) SyncGames(_ context.Context) ([]models.Game, error) {
	return m.games, m.err
}

type mockRepo struct {
	squares      []models.Square
	roundConfigs []models.RoundConfig
	axes         map[string]models.Axis // key: "roundNum:type"
	payoutExists map[string]bool        // key: "gameID:row:col"
	putPayouts   []models.Payout
	syncStateAt  time.Time
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		axes:         make(map[string]models.Axis),
		payoutExists: make(map[string]bool),
	}
}

func (m *mockRepo) GetAllSquares(_ context.Context, _ string) ([]models.Square, error) {
	return m.squares, nil
}
func (m *mockRepo) GetAllRoundConfigs(_ context.Context, _ string) ([]models.RoundConfig, error) {
	return m.roundConfigs, nil
}
func (m *mockRepo) GetRoundAxis(_ context.Context, _ string, roundNum int, axisType string) (models.Axis, error) {
	key := fmt.Sprintf("%d:%s", roundNum, axisType)
	ax, ok := m.axes[key]
	if !ok {
		return models.Axis{}, fmt.Errorf("axis not found")
	}
	return ax, nil
}
func (m *mockRepo) PayoutExists(_ context.Context, _, gameID string, row, col int) (bool, error) {
	key := fmt.Sprintf("%s:%d:%d", gameID, row, col)
	return m.payoutExists[key], nil
}
func (m *mockRepo) PutPayout(_ context.Context, p models.Payout) error {
	m.putPayouts = append(m.putPayouts, p)
	return nil
}
func (m *mockRepo) PutSyncState(_ context.Context, _ string, t time.Time) error {
	m.syncStateAt = t
	return nil
}

// --- Tests ---

func TestSync_CreatesPayout(t *testing.T) {
	// Setup: one final game, matching square owner, axes with sequential digits
	digits := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	repo := newMockRepo()
	repo.squares = []models.Square{{PoolID: "p1", Row: 8, Col: 3, OwnerName: "Alice"}}
	repo.roundConfigs = []models.RoundConfig{{PoolID: "p1", RoundNum: 1, PayoutAmount: 10.0}}
	repo.axes["1:winner"] = models.Axis{Digits: digits}
	repo.axes["1:loser"] = models.Axis{Digits: digits}

	espnMock := &mockESPN{
		games: []models.Game{
			{
				EspnID:      "g1",
				Status:      "final",
				RoundNum:    1,
				WinnerScore: 73, // last digit 3 → col 3
				LoserScore:  68, // last digit 8 → row 8
			},
		},
	}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}

	if len(repo.putPayouts) != 1 {
		t.Fatalf("expected 1 payout, got %d", len(repo.putPayouts))
	}
	p := repo.putPayouts[0]
	if p.OwnerName != "Alice" {
		t.Errorf("payout owner = %q, want Alice", p.OwnerName)
	}
	if p.Amount != 10.0 {
		t.Errorf("payout amount = %f, want 10.0", p.Amount)
	}
	if p.Row != 8 || p.Col != 3 {
		t.Errorf("payout (row,col) = (%d,%d), want (8,3)", p.Row, p.Col)
	}
	if p.GameID != "g1" {
		t.Errorf("payout gameID = %q, want g1", p.GameID)
	}
}

func TestSync_SkipsNonFinalGames(t *testing.T) {
	repo := newMockRepo()
	repo.squares = []models.Square{{Row: 0, Col: 0, OwnerName: "Bob"}}
	repo.roundConfigs = []models.RoundConfig{{RoundNum: 1, PayoutAmount: 5}}
	digits := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	repo.axes["1:winner"] = models.Axis{Digits: digits}
	repo.axes["1:loser"] = models.Axis{Digits: digits}

	espnMock := &mockESPN{
		games: []models.Game{
			{EspnID: "g1", Status: "in_progress", RoundNum: 1, WinnerScore: 50, LoserScore: 40},
			{EspnID: "g2", Status: "scheduled", RoundNum: 1, WinnerScore: 0, LoserScore: 0},
		},
	}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}
	if len(repo.putPayouts) != 0 {
		t.Errorf("expected 0 payouts for non-final games, got %d", len(repo.putPayouts))
	}
}

func TestSync_DeduplicatesPayout(t *testing.T) {
	digits := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	repo := newMockRepo()
	repo.squares = []models.Square{{Row: 8, Col: 3, OwnerName: "Alice"}}
	repo.roundConfigs = []models.RoundConfig{{RoundNum: 1, PayoutAmount: 10}}
	repo.axes["1:winner"] = models.Axis{Digits: digits}
	repo.axes["1:loser"] = models.Axis{Digits: digits}
	// Mark this payout as already existing
	repo.payoutExists["g1:8:3"] = true

	espnMock := &mockESPN{
		games: []models.Game{
			{EspnID: "g1", Status: "final", RoundNum: 1, WinnerScore: 73, LoserScore: 68},
		},
	}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}
	if len(repo.putPayouts) != 0 {
		t.Errorf("expected 0 payouts (duplicate), got %d", len(repo.putPayouts))
	}
}

func TestSync_WritesSyncState(t *testing.T) {
	repo := newMockRepo()
	espnMock := &mockESPN{games: nil}

	s := New(repo, espnMock)
	before := time.Now().UTC()
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}

	if repo.syncStateAt.IsZero() {
		t.Error("sync state was not written")
	}
	if repo.syncStateAt.Before(before) {
		t.Error("sync state timestamp is before the sync call")
	}
}

func TestSync_ESPNError(t *testing.T) {
	repo := newMockRepo()
	espnMock := &mockESPN{err: fmt.Errorf("network error")}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err == nil {
		t.Error("expected error from ESPN failure")
	}
}

func TestSync_NoSquareOwner(t *testing.T) {
	// Game resolves to a square with no owner — no payout created
	digits := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	repo := newMockRepo()
	// No squares at all
	repo.roundConfigs = []models.RoundConfig{{RoundNum: 1, PayoutAmount: 10}}
	repo.axes["1:winner"] = models.Axis{Digits: digits}
	repo.axes["1:loser"] = models.Axis{Digits: digits}

	espnMock := &mockESPN{
		games: []models.Game{
			{EspnID: "g1", Status: "final", RoundNum: 1, WinnerScore: 73, LoserScore: 68},
		},
	}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}
	if len(repo.putPayouts) != 0 {
		t.Errorf("expected 0 payouts (no square owner), got %d", len(repo.putPayouts))
	}
}

func TestSync_InvalidRoundClampedTo1(t *testing.T) {
	// Game with roundNum=0 should be treated as round 1
	digits := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	repo := newMockRepo()
	repo.squares = []models.Square{{Row: 5, Col: 7, OwnerName: "Carol"}}
	repo.roundConfigs = []models.RoundConfig{{RoundNum: 1, PayoutAmount: 25}}
	repo.axes["1:winner"] = models.Axis{Digits: digits}
	repo.axes["1:loser"] = models.Axis{Digits: digits}

	espnMock := &mockESPN{
		games: []models.Game{
			{EspnID: "g1", Status: "final", RoundNum: 0, WinnerScore: 77, LoserScore: 65},
		},
	}

	s := New(repo, espnMock)
	err := s.Sync(context.Background(), "p1")
	if err != nil {
		t.Fatalf("Sync error: %v", err)
	}
	if len(repo.putPayouts) != 1 {
		t.Fatalf("expected 1 payout, got %d", len(repo.putPayouts))
	}
	if repo.putPayouts[0].Amount != 25 {
		t.Errorf("payout amount = %f, want 25 (from round 1 config)", repo.putPayouts[0].Amount)
	}
}
