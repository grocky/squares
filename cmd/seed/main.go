package main

import (
	"context"
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
	ddb := dynamodb.NewFromConfig(cfg)
	repo := dynrepo.NewRepo(ddb)

	poolID := "main"

	// Create pool
	pool := models.Pool{
		ID:           poolID,
		Name:         "2025 NCAA Tournament",
		PayoutAmount: 25.00,
		Status:       "active",
		CreatedAt:    time.Now().UTC(),
	}
	if err := repo.PutPool(ctx, pool); err != nil {
		log.Fatalf("failed to create pool: %v", err)
	}
	log.Println("Created pool:", pool.Name)

	// Assign axes (seeded from pool ID)
	var seed int64
	for _, c := range poolID {
		seed = seed*31 + int64(c)
	}
	rng := rand.New(rand.NewSource(seed))
	rowDigits := rng.Perm(10)
	colDigits := rng.Perm(10)

	if err := repo.PutAxis(ctx, models.Axis{PoolID: poolID, Type: "row", Digits: rowDigits}); err != nil {
		log.Fatalf("failed to create row axis: %v", err)
	}
	if err := repo.PutAxis(ctx, models.Axis{PoolID: poolID, Type: "col", Digits: colDigits}); err != nil {
		log.Fatalf("failed to create col axis: %v", err)
	}
	log.Printf("Row axis: %v", rowDigits)
	log.Printf("Col axis: %v", colDigits)

	// Assign squares: 20 owners, 5 squares each = 100
	owners := []string{
		"Rocky", "Alice", "Bob", "Charlie", "Diana",
		"Eve", "Frank", "Grace", "Hank", "Ivy",
		"Jack", "Karen", "Leo", "Mona", "Nick",
		"Olivia", "Pete", "Quinn", "Rita", "Sam",
	}

	// Rocky gets specific squares: (3,7), (6,2), (8,0)
	rockySquares := [][2]int{{3, 7}, {6, 2}, {8, 0}}
	assigned := make(map[[2]int]bool)
	for _, rc := range rockySquares {
		sq := models.Square{PoolID: poolID, Row: rc[0], Col: rc[1], OwnerName: "Rocky"}
		if err := repo.PutSquare(ctx, sq); err != nil {
			log.Fatalf("failed to assign square: %v", err)
		}
		assigned[rc] = true
	}

	// Build list of remaining cells
	var remaining [][2]int
	for r := 0; r < 10; r++ {
		for c := 0; c < 10; c++ {
			if !assigned[[2]int{r, c}] {
				remaining = append(remaining, [2]int{r, c})
			}
		}
	}
	rng.Shuffle(len(remaining), func(i, j int) {
		remaining[i], remaining[j] = remaining[j], remaining[i]
	})

	// Rocky needs 2 more squares (already has 3, needs 5 total)
	for i := 0; i < 2; i++ {
		rc := remaining[i]
		sq := models.Square{PoolID: poolID, Row: rc[0], Col: rc[1], OwnerName: "Rocky"}
		if err := repo.PutSquare(ctx, sq); err != nil {
			log.Fatalf("failed to assign square: %v", err)
		}
		assigned[rc] = true
	}
	remaining = remaining[2:]

	// Assign 5 squares each to the other 19 owners
	idx := 0
	for _, owner := range owners[1:] {
		for j := 0; j < 5; j++ {
			rc := remaining[idx]
			sq := models.Square{PoolID: poolID, Row: rc[0], Col: rc[1], OwnerName: owner}
			if err := repo.PutSquare(ctx, sq); err != nil {
				log.Fatalf("failed to assign square: %v", err)
			}
			idx++
		}
	}

	fmt.Printf("Seeded pool %q with %d squares across %d owners\n", poolID, 100, len(owners))
}
