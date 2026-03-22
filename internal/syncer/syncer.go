package syncer

import (
	"context"
	"log"
	"time"

	"github.com/grocky/squares/internal/models"
	"github.com/grocky/squares/internal/scorer"
)

// Repository defines the data access methods the syncer needs.
type Repository interface {
	GetAllSquares(ctx context.Context, poolID string) ([]models.Square, error)
	GetAllRoundConfigs(ctx context.Context, poolID string) ([]models.RoundConfig, error)
	GetRoundAxis(ctx context.Context, poolID string, roundNum int, axisType string) (models.Axis, error)
	PayoutExists(ctx context.Context, poolID, gameID string, row, col int) (bool, error)
	PutPayout(ctx context.Context, p models.Payout) error
	PutSyncState(ctx context.Context, poolID string, syncedAt time.Time) error
}

// ESPNClient defines the ESPN data fetching method the syncer needs.
type ESPNClient interface {
	SyncGames(ctx context.Context, poolID string) ([]models.Game, error)
}

type Syncer struct {
	repo       Repository
	espnClient ESPNClient
}

func New(repo Repository, espnClient ESPNClient) *Syncer {
	return &Syncer{repo: repo, espnClient: espnClient}
}

// Sync fetches games from ESPN, computes payouts for final games, and stores them.
func (s *Syncer) Sync(ctx context.Context, poolID string) error {
	games, err := s.espnClient.SyncGames(ctx, poolID)
	if err != nil {
		return err
	}

	squares, err := s.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		return err
	}
	squareMap := make(map[[2]int]models.Square)
	for _, sq := range squares {
		squareMap[[2]int{sq.Row, sq.Col}] = sq
	}

	roundConfigs, err := s.repo.GetAllRoundConfigs(ctx, poolID)
	if err != nil {
		return err
	}
	rcMap := make(map[int]models.RoundConfig)
	for _, rc := range roundConfigs {
		rcMap[rc.RoundNum] = rc
	}

	for _, game := range games {
		if game.Status != "final" {
			continue
		}
		roundNum := game.RoundNum
		if roundNum < 1 || roundNum > 6 {
			roundNum = 1
		}

		winnerAxis, err := s.repo.GetRoundAxis(ctx, poolID, roundNum, "winner")
		if err != nil {
			log.Printf("no winner axis for round %d: %v", roundNum, err)
			continue
		}
		loserAxis, err := s.repo.GetRoundAxis(ctx, poolID, roundNum, "loser")
		if err != nil {
			log.Printf("no loser axis for round %d: %v", roundNum, err)
			continue
		}

		row, col := scorer.FindWinningSquare(game, winnerAxis, loserAxis)
		if row < 0 || col < 0 {
			continue
		}

		exists, err := s.repo.PayoutExists(ctx, poolID, game.EspnID, row, col)
		if err != nil {
			log.Printf("error checking payout: %v", err)
			continue
		}
		if exists {
			continue
		}

		sq, ok := squareMap[[2]int{row, col}]
		if !ok {
			continue
		}

		payoutAmount := 0.0
		if rc, ok := rcMap[roundNum]; ok {
			payoutAmount = rc.PayoutAmount
		}

		payout := models.Payout{
			PoolID:      poolID,
			GameID:      game.EspnID,
			Row:         row,
			Col:         col,
			OwnerName:   sq.OwnerName,
			Amount:      payoutAmount,
			WinnerScore: game.WinnerScore,
			LoserScore:  game.LoserScore,
		}
		if err := s.repo.PutPayout(ctx, payout); err != nil {
			log.Printf("error creating payout: %v", err)
		}
	}

	// Write sync timestamp so the server can detect changes via polling
	// instead of requiring an inbound HTTP call from the Lambda.
	if err := s.repo.PutSyncState(ctx, poolID, time.Now().UTC()); err != nil {
		log.Printf("warning: failed to write sync state: %v", err)
	}

	return nil
}
