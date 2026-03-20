// dump reads the current DynamoDB state and writes it to config/seed.json.
// Run: make dump-seed
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"sort"

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

	poolID := "main"

	pool, err := repo.GetPool(ctx, poolID)
	if err != nil {
		log.Fatalf("failed to get pool: %v", err)
	}

	roundConfigs, err := repo.GetAllRoundConfigs(ctx, poolID)
	if err != nil {
		log.Fatalf("failed to get round configs: %v", err)
	}
	sort.Slice(roundConfigs, func(i, j int) bool {
		return roundConfigs[i].RoundNum < roundConfigs[j].RoundNum
	})

	var axes []axisPair
	for roundNum := 1; roundNum <= 6; roundNum++ {
		w, wErr := repo.GetRoundAxis(ctx, poolID, roundNum, "winner")
		l, lErr := repo.GetRoundAxis(ctx, poolID, roundNum, "loser")
		if wErr != nil || lErr != nil {
			log.Printf("warning: missing axes for round %d, skipping", roundNum)
			continue
		}
		axes = append(axes, axisPair{RoundNum: roundNum, Winner: w.Digits, Loser: l.Digits})
	}

	squares, err := repo.GetAllSquares(ctx, poolID)
	if err != nil {
		log.Fatalf("failed to get squares: %v", err)
	}
	sort.Slice(squares, func(i, j int) bool {
		if squares[i].Row != squares[j].Row {
			return squares[i].Row < squares[j].Row
		}
		return squares[i].Col < squares[j].Col
	})

	sc := seedConfig{
		Pool:         pool,
		RoundConfigs: roundConfigs,
		Axes:         axes,
		Squares:      squares,
	}

	if err := os.MkdirAll("config", 0755); err != nil {
		log.Fatalf("failed to create config dir: %v", err)
	}

	f, err := os.Create("config/seed.json")
	if err != nil {
		log.Fatalf("failed to create seed file: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(sc); err != nil {
		log.Fatalf("failed to write seed file: %v", err)
	}

	log.Printf("✅ Seed config written to config/seed.json (%d squares, %d round configs, %d round axes)",
		len(squares), len(roundConfigs), len(axes))
}
