package scorer

import (
	"testing"

	"github.com/grocky/squares/internal/models"
)

func TestFindWinningSquare_BasicMatch(t *testing.T) {
	// WinnerScore=73, LoserScore=64
	// last digits: winner=3, loser=4
	// winnerAxis.Digits: [0,1,2,3,4,5,6,7,8,9] → index of 3 is 3 (col)
	// loserAxis.Digits:  [0,1,2,3,4,5,6,7,8,9] → index of 4 is 4 (row)
	game := models.Game{WinnerScore: 73, LoserScore: 64}
	winnerAxis := models.Axis{Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}
	loserAxis := models.Axis{Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}

	row, col := FindWinningSquare(game, winnerAxis, loserAxis)
	if row != 4 || col != 3 {
		t.Errorf("FindWinningSquare = (%d, %d), want (4, 3)", row, col)
	}
}

func TestFindWinningSquare_ShuffledAxes(t *testing.T) {
	// WinnerScore=80, LoserScore=71 → last digits: 0, 1
	// winnerAxis: [5,0,3,7,1,9,2,6,8,4] → 0 is at index 1 (col)
	// loserAxis:  [9,8,7,6,5,4,3,2,1,0] → 1 is at index 8 (row)
	game := models.Game{WinnerScore: 80, LoserScore: 71}
	winnerAxis := models.Axis{Digits: []int{5, 0, 3, 7, 1, 9, 2, 6, 8, 4}}
	loserAxis := models.Axis{Digits: []int{9, 8, 7, 6, 5, 4, 3, 2, 1, 0}}

	row, col := FindWinningSquare(game, winnerAxis, loserAxis)
	if row != 8 || col != 1 {
		t.Errorf("FindWinningSquare = (%d, %d), want (8, 1)", row, col)
	}
}

func TestFindWinningSquare_ZeroScores(t *testing.T) {
	game := models.Game{WinnerScore: 0, LoserScore: 0}
	axis := models.Axis{Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}
	row, col := FindWinningSquare(game, axis, axis)
	if row != 0 || col != 0 {
		t.Errorf("FindWinningSquare = (%d, %d), want (0, 0)", row, col)
	}
}

func TestFindWinningSquare_DigitNotInAxis(t *testing.T) {
	// Axis missing digit 5
	game := models.Game{WinnerScore: 75, LoserScore: 60}
	winnerAxis := models.Axis{Digits: []int{0, 1, 2, 3, 4, 6, 7, 8, 9}} // missing 5
	loserAxis := models.Axis{Digits: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}}

	row, col := FindWinningSquare(game, winnerAxis, loserAxis)
	if col != -1 {
		t.Errorf("col = %d, want -1 for missing digit", col)
	}
	if row != 0 {
		t.Errorf("row = %d, want 0", row)
	}
}
