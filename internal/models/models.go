package models

import "time"

type Pool struct {
	ID        string
	Name      string
	Status    string // "active", "complete"
	CreatedAt time.Time
}

type RoundConfig struct {
	PoolID       string
	RoundNum     int
	Name         string
	PayoutAmount float64
}

// DefaultRoundConfigs returns the 6 NCAA tournament rounds with default payouts.
func DefaultRoundConfigs() []RoundConfig {
	return []RoundConfig{
		{RoundNum: 1, Name: "Round of 64", PayoutAmount: 10},
		{RoundNum: 2, Name: "Round of 32", PayoutAmount: 20},
		{RoundNum: 3, Name: "Sweet 16", PayoutAmount: 30},
		{RoundNum: 4, Name: "Elite 8", PayoutAmount: 50},
		{RoundNum: 5, Name: "Final Four", PayoutAmount: 100},
		{RoundNum: 6, Name: "Championship", PayoutAmount: 200},
	}
}

type Axis struct {
	PoolID   string
	RoundNum int
	Type     string // "winner" or "loser"
	Digits   []int  // shuffled 0-9
}

type Square struct {
	PoolID    string
	Row       int
	Col       int
	OwnerName string
}

type Game struct {
	EspnID      string
	HomeTeam    string
	AwayTeam    string
	Round       string
	RoundNum    int
	HomeScore   int
	AwayScore   int
	HomeRank    int
	AwayRank    int
	WinnerScore int    // derived: max(HomeScore, AwayScore) — used by scorer
	LoserScore  int    // derived: min(HomeScore, AwayScore) — used by scorer
	Status      string // "scheduled", "in_progress", "final"
	StartTime   time.Time
	SyncedAt    time.Time
}

type Payout struct {
	PoolID      string
	GameID      string
	Row         int
	Col         int
	OwnerName   string
	Amount      float64
	WinnerScore int
	LoserScore  int
}
