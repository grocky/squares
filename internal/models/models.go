package models

import "time"

type Pool struct {
	ID           string
	Name         string
	PayoutAmount float64 // dollars per winning square per game
	Status       string  // "active", "complete"
	CreatedAt    time.Time
}

type Axis struct {
	PoolID string
	Type   string // "row" or "col"
	Digits []int  // shuffled 0-9
}

type Square struct {
	PoolID    string
	Row       int
	Col       int
	OwnerName string
}

type Game struct {
	PoolID    string
	EspnID    string
	HomeTeam  string
	AwayTeam  string
	Round     string
	HomeScore int
	AwayScore int
	Status    string // "scheduled", "in_progress", "final"
	SyncedAt  time.Time
}

type Payout struct {
	PoolID    string
	GameID    string
	Row       int
	Col       int
	OwnerName string
	Amount    float64
	HomeScore int
	AwayScore int
}
