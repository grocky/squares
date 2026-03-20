// seed loads pool configuration from config/seed.json into DynamoDB.
// Run: make seed
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynrepo "github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/models"
)

type axisPair struct {
	RoundNum int   `json:"roundNum"`
	Winner   []int `json:"winner"`
	Loser    []int `json:"loser"`
}

type seedConfig struct {
	Pool         models.Pool          `json:"pool"`
	RoundConfigs []models.RoundConfig `json:"roundConfigs"`
	Axes         []axisPair           `json:"axes"`
	Squares      []models.Square      `json:"squares"`
}

func main() {
	ctx := context.Background()

	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		log.Fatalf("unable to load AWS config: %v", err)
	}
	repo := dynrepo.NewRepo(dynamodb.NewFromConfig(cfg))

	sc, err := loadSeedConfig("config/seed.json")
	if err != nil {
		log.Printf("no seed config found (%v), using defaults", err)
		sc = defaultSeedConfig()
	}

	poolID := "main"
	sc.Pool.ID = poolID
	sc.Pool.CreatedAt = time.Now().UTC()

	// Pool
	if err := repo.PutPool(ctx, sc.Pool); err != nil {
		log.Fatalf("failed to create pool: %v", err)
	}
	log.Println("Created pool:", sc.Pool.Name)

	// Round configs
	for _, rc := range sc.RoundConfigs {
		rc.PoolID = poolID
		if err := repo.PutRoundConfig(ctx, rc); err != nil {
			log.Fatalf("failed to create round config %d: %v", rc.RoundNum, err)
		}
		log.Printf("Round %d (%s): $%.0f", rc.RoundNum, rc.Name, rc.PayoutAmount)
	}

	// Axes
	for _, ap := range sc.Axes {
		if err := repo.PutRoundAxis(ctx, models.Axis{PoolID: poolID, RoundNum: ap.RoundNum, Type: "winner", Digits: ap.Winner}); err != nil {
			log.Fatalf("failed to save winner axis round %d: %v", ap.RoundNum, err)
		}
		if err := repo.PutRoundAxis(ctx, models.Axis{PoolID: poolID, RoundNum: ap.RoundNum, Type: "loser", Digits: ap.Loser}); err != nil {
			log.Fatalf("failed to save loser axis round %d: %v", ap.RoundNum, err)
		}
		log.Printf("Round %d axes seeded", ap.RoundNum)
	}

	// Squares
	for _, sq := range sc.Squares {
		sq.PoolID = poolID
		if err := repo.PutSquare(ctx, sq); err != nil {
			log.Fatalf("failed to assign square (%d,%d): %v", sq.Row, sq.Col, err)
		}
	}

	fmt.Printf("Seeded pool %q with %d squares\n", poolID, len(sc.Squares))
}

func loadSeedConfig(path string) (seedConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return seedConfig{}, err
	}
	defer f.Close()

	var sc seedConfig
	if err := json.NewDecoder(f).Decode(&sc); err != nil {
		return seedConfig{}, fmt.Errorf("invalid seed config: %w", err)
	}
	return sc, nil
}

func defaultSeedConfig() seedConfig {
	poolID := "main"

	var seed int64
	for _, c := range poolID {
		seed = seed*31 + int64(c)
	}
	rng := rand.New(rand.NewSource(seed))

	var axes []axisPair
	for roundNum := 1; roundNum <= 6; roundNum++ {
		axes = append(axes, axisPair{
			RoundNum: roundNum,
			Winner:   rng.Perm(10),
			Loser:    rng.Perm(10),
		})
	}

	owners := []string{
		"Rocky", "Alice", "Bob", "Charlie", "Diana",
		"Eve", "Frank", "Grace", "Hank", "Ivy",
		"Jack", "Karen", "Leo", "Mona", "Nick",
		"Olivia", "Pete", "Quinn", "Rita", "Sam",
	}

	rockySquares := [][2]int{{3, 7}, {6, 2}, {8, 0}}
	assigned := make(map[[2]int]bool)
	var squares []models.Square
	for _, rc := range rockySquares {
		squares = append(squares, models.Square{Row: rc[0], Col: rc[1], OwnerName: "Rocky"})
		assigned[rc] = true
	}

	var remaining [][2]int
	for r := 0; r < 10; r++ {
		for c := 0; c < 10; c++ {
			if !assigned[[2]int{r, c}] {
				remaining = append(remaining, [2]int{r, c})
			}
		}
	}
	rng.Shuffle(len(remaining), func(i, j int) { remaining[i], remaining[j] = remaining[j], remaining[i] })

	for i := 0; i < 2; i++ {
		rc := remaining[i]
		squares = append(squares, models.Square{Row: rc[0], Col: rc[1], OwnerName: "Rocky"})
		assigned[rc] = true
	}
	remaining = remaining[2:]

	idx := 0
	for _, owner := range owners[1:] {
		for j := 0; j < 5; j++ {
			rc := remaining[idx]
			squares = append(squares, models.Square{Row: rc[0], Col: rc[1], OwnerName: owner})
			idx++
		}
	}

	return seedConfig{
		Pool:         models.Pool{Name: "2025 NCAA Tournament", Status: "active"},
		RoundConfigs: models.DefaultRoundConfigs(),
		Axes:         axes,
		Squares:      squares,
	}
}
