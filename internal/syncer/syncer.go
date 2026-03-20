package syncer

import (
	"context"
	"log"

	"github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/models"
	"github.com/grocky/squares/internal/scorer"
)

type Syncer struct {
	repo       *dynamo.Repo
	espnClient *espn.Client
}

func New(repo *dynamo.Repo, espnClient *espn.Client) *Syncer {
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

	return nil
}
