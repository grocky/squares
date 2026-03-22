package api

import (
	"context"
	"fmt"
	"sort"

	"github.com/grocky/squares/internal/models"
)

// buildAxesData builds roundAxes pairs and selects the display axes for the given round.
func buildAxesData(allAxes []models.Axis, roundConfigs []models.RoundConfig, roundFilter int) ([]roundAxisPair, models.Axis, models.Axis, bool) {
	rcMap := make(map[int]string)
	for _, rc := range roundConfigs {
		rcMap[rc.RoundNum] = rc.Name
	}

	winnerAxes := make(map[int]models.Axis)
	loserAxes := make(map[int]models.Axis)
	for _, ax := range allAxes {
		if ax.Type == "winner" {
			winnerAxes[ax.RoundNum] = ax
		} else {
			loserAxes[ax.RoundNum] = ax
		}
	}

	var roundAxes []roundAxisPair
	for roundNum := 1; roundNum <= 6; roundNum++ {
		wa, wOk := winnerAxes[roundNum]
		la, lOk := loserAxes[roundNum]
		if wOk && lOk {
			name := rcMap[roundNum]
			if name == "" {
				name = fmt.Sprintf("Round %d", roundNum)
			}
			roundAxes = append(roundAxes, roundAxisPair{
				RoundNum:   roundNum,
				RoundName:  name,
				WinnerAxis: wa,
				LoserAxis:  la,
			})
		}
	}

	displayRound := roundFilter
	if displayRound == 0 {
		displayRound = 1
	}
	var winnerAxis, loserAxis models.Axis
	var hasAxes bool
	for _, ra := range roundAxes {
		if ra.RoundNum == displayRound {
			winnerAxis = ra.WinnerAxis
			loserAxis = ra.LoserAxis
			hasAxes = true
			break
		}
	}
	if !hasAxes && len(roundAxes) > 0 {
		winnerAxis = roundAxes[0].WinnerAxis
		loserAxis = roundAxes[0].LoserAxis
		hasAxes = true
	}

	return roundAxes, winnerAxis, loserAxis, hasAxes
}

// buildGrid populates the 10x10 grid from squares.
func buildGrid(squares []models.Square) [10][10]gridCell {
	var grid [10][10]gridCell
	for _, sq := range squares {
		if sq.Row >= 0 && sq.Row < 10 && sq.Col >= 0 && sq.Col < 10 {
			grid[sq.Row][sq.Col] = gridCell{OwnerName: sq.OwnerName}
		}
	}
	return grid
}

// applyPayoutsToGrid marks winner cells and accumulates amounts on the grid.
func applyPayoutsToGrid(grid *[10][10]gridCell, payouts []models.Payout) {
	for _, p := range payouts {
		if p.Row >= 0 && p.Row < 10 && p.Col >= 0 && p.Col < 10 {
			cell := &grid[p.Row][p.Col]
			cell.IsWinner = true
			cell.Amount += p.Amount
		}
	}
}

// processPayouts loads all payouts, overrides amounts from round configs, and
// optionally filters by round using the game→round map.
func processPayouts(allPayouts []models.Payout, roundConfigs []models.RoundConfig, gameRoundMap map[string]int, roundFilter int, filteredGameIDs map[string]bool) []models.Payout {
	rcPayoutMap := make(map[int]float64)
	for _, rc := range roundConfigs {
		rcPayoutMap[rc.RoundNum] = rc.PayoutAmount
	}
	for i, p := range allPayouts {
		if roundNum, ok := gameRoundMap[p.GameID]; ok {
			if currentAmount, ok := rcPayoutMap[roundNum]; ok {
				allPayouts[i].Amount = currentAmount
			}
		}
	}

	if roundFilter > 0 {
		var filtered []models.Payout
		for _, p := range allPayouts {
			if filteredGameIDs[p.GameID] {
				filtered = append(filtered, p)
			}
		}
		return filtered
	}
	return allPayouts
}

// buildLeaderboard computes the leaderboard from all payouts across all rounds.
func buildLeaderboard(allPayouts []models.Payout) []leaderEntry {
	totals := make(map[string]*leaderEntry)
	for _, p := range allPayouts {
		e, ok := totals[p.OwnerName]
		if !ok {
			e = &leaderEntry{Name: p.OwnerName}
			totals[p.OwnerName] = e
		}
		e.Total += p.Amount
		e.Wins++
	}
	var board []leaderEntry
	for _, e := range totals {
		board = append(board, *e)
	}
	sort.Slice(board, func(i, j int) bool {
		a, b := board[i], board[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		return a.Name < b.Name
	})
	return board
}

// loadFullDashboard fetches all data needed for the full page render.
// Used by handlePoolDashboard and handleAdminDashboard.
func (h *Handler) loadFullDashboard(ctx context.Context, poolID string, roundFilter int) (dashboardData, error) {
	pool, err := h.repo.GetPool(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}

	roundConfigs, _ := h.repo.GetAllRoundConfigs(ctx, poolID)
	allAxes, _ := h.repo.GetAllRoundAxes(ctx, poolID)
	roundAxes, winnerAxis, loserAxis, hasAxes := buildAxesData(allAxes, roundConfigs, roundFilter)

	squares, err := h.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	grid := buildGrid(squares)

	allGames, err := h.repo.GetAllGamesGlobal(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	gameRoundMap := make(map[string]int)
	for _, g := range allGames {
		gameRoundMap[g.EspnID] = g.RoundNum
	}

	var games []models.Game
	filteredGameIDs := make(map[string]bool)
	if roundFilter > 0 {
		for _, g := range allGames {
			if g.RoundNum == roundFilter {
				games = append(games, g)
				filteredGameIDs[g.EspnID] = true
			}
		}
	} else {
		games = allGames
	}
	sort.Slice(games, func(i, j int) bool {
		return games[i].StartTime.Before(games[j].StartTime)
	})

	allPayouts, err := h.repo.GetAllPayouts(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	payouts := processPayouts(allPayouts, roundConfigs, gameRoundMap, roundFilter, filteredGameIDs)
	applyPayoutsToGrid(&grid, payouts)
	leaderboard := buildLeaderboard(allPayouts)

	return dashboardData{
		Pool:         pool,
		WinnerAxis:   winnerAxis,
		LoserAxis:    loserAxis,
		Grid:         grid,
		Payouts:      payouts,
		Leaderboard:  leaderboard,
		Games:        games,
		HasAxes:      hasAxes,
		RoundConfigs: roundConfigs,
		RoundAxes:    roundAxes,
		RoundFilter:  roundFilter,
	}, nil
}

// loadGridData fetches axes, squares, payouts, and games for the selected round.
// Used by handleGrid, handleUpdateSquare, handleUpdateRoundAxis.
func (h *Handler) loadGridData(ctx context.Context, poolID string, roundFilter int) (dashboardData, error) {
	roundConfigs, _ := h.repo.GetAllRoundConfigs(ctx, poolID)
	allAxes, _ := h.repo.GetAllRoundAxes(ctx, poolID)
	roundAxes, winnerAxis, loserAxis, hasAxes := buildAxesData(allAxes, roundConfigs, roundFilter)

	squares, err := h.repo.GetAllSquares(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	grid := buildGrid(squares)

	// Fetch games for the selected round (needed for payout amount override)
	allGames, err := h.repo.GetAllGamesGlobal(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	gameRoundMap := make(map[string]int)
	filteredGameIDs := make(map[string]bool)
	var games []models.Game
	for _, g := range allGames {
		gameRoundMap[g.EspnID] = g.RoundNum
		if roundFilter > 0 && g.RoundNum == roundFilter {
			filteredGameIDs[g.EspnID] = true
			games = append(games, g)
		}
	}
	if roundFilter == 0 {
		games = allGames
	}
	sort.Slice(games, func(i, j int) bool {
		return games[i].StartTime.Before(games[j].StartTime)
	})

	allPayouts, err := h.repo.GetAllPayouts(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	payouts := processPayouts(allPayouts, roundConfigs, gameRoundMap, roundFilter, filteredGameIDs)
	applyPayoutsToGrid(&grid, payouts)

	return dashboardData{
		WinnerAxis:   winnerAxis,
		LoserAxis:    loserAxis,
		Grid:         grid,
		Payouts:      payouts,
		HasAxes:      hasAxes,
		Games:        games,
		RoundConfigs: roundConfigs,
		RoundAxes:    roundAxes,
		RoundFilter:  roundFilter,
	}, nil
}

// loadLeaderboardData fetches only payouts (all rounds) for the leaderboard.
// Used by handleLeaderboard.
func (h *Handler) loadLeaderboardData(ctx context.Context, poolID string) (dashboardData, error) {
	roundConfigs, _ := h.repo.GetAllRoundConfigs(ctx, poolID)

	allGames, err := h.repo.GetAllGamesGlobal(ctx)
	if err != nil {
		return dashboardData{}, err
	}
	gameRoundMap := make(map[string]int)
	for _, g := range allGames {
		gameRoundMap[g.EspnID] = g.RoundNum
	}

	allPayouts, err := h.repo.GetAllPayouts(ctx, poolID)
	if err != nil {
		return dashboardData{}, err
	}
	// Override amounts with current round config values
	rcPayoutMap := make(map[int]float64)
	for _, rc := range roundConfigs {
		rcPayoutMap[rc.RoundNum] = rc.PayoutAmount
	}
	for i, p := range allPayouts {
		if roundNum, ok := gameRoundMap[p.GameID]; ok {
			if currentAmount, ok := rcPayoutMap[roundNum]; ok {
				allPayouts[i].Amount = currentAmount
			}
		}
	}

	leaderboard := buildLeaderboard(allPayouts)

	return dashboardData{
		Leaderboard: leaderboard,
	}, nil
}

// loadGamesData fetches global games filtered by round.
// Used by handleGames.
func (h *Handler) loadGamesData(ctx context.Context, roundFilter int) (dashboardData, error) {
	allGames, err := h.repo.GetAllGamesGlobal(ctx)
	if err != nil {
		return dashboardData{}, err
	}

	var games []models.Game
	if roundFilter > 0 {
		for _, g := range allGames {
			if g.RoundNum == roundFilter {
				games = append(games, g)
			}
		}
	} else {
		games = allGames
	}
	sort.Slice(games, func(i, j int) bool {
		return games[i].StartTime.Before(games[j].StartTime)
	})

	return dashboardData{
		Games:       games,
		RoundFilter: roundFilter,
	}, nil
}
