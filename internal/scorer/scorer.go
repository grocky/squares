package scorer

import "github.com/grocky/squares/internal/models"

// FindWinningSquare returns the (row, col) indices of the winning square
// given a game's final scores and the pool's row/col axes.
// row = index of (homeScore % 10) in rowAxis.Digits
// col = index of (awayScore % 10) in colAxis.Digits
func FindWinningSquare(game models.Game, rowAxis models.Axis, colAxis models.Axis) (int, int) {
	homeLastDigit := game.HomeScore % 10
	awayLastDigit := game.AwayScore % 10

	row := indexOf(rowAxis.Digits, homeLastDigit)
	col := indexOf(colAxis.Digits, awayLastDigit)

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
