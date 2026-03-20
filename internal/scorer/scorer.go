package scorer

import "github.com/grocky/squares/internal/models"

// FindWinningSquare returns the (row, col) indices of the winning square
// given a game's final scores and the round's winner/loser axes.
// col = index of (WinnerScore % 10) in winnerAxis.Digits (columns)
// row = index of (LoserScore % 10) in loserAxis.Digits (rows)
func FindWinningSquare(game models.Game, winnerAxis models.Axis, loserAxis models.Axis) (int, int) {
	winnerLastDigit := game.WinnerScore % 10
	loserLastDigit := game.LoserScore % 10

	col := indexOf(winnerAxis.Digits, winnerLastDigit)
	row := indexOf(loserAxis.Digits, loserLastDigit)

	return row, col
}

func indexOf(digits []int, target int) int {
	for i, d := range digits {
		if d == target {
			return i
		}
	}
	return -1
}
