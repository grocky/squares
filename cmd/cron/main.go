package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynrepo "github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/sse"
	"github.com/grocky/squares/internal/syncer"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region()))
	if err != nil {
		log.Fatalf("unable to load AWS config: %v", err)
	}
	ddb := dynamodb.NewFromConfig(cfg)
	repo := dynrepo.NewRepo(ddb)
	espnClient := espn.NewClient(repo)
	s := syncer.New(repo, espnClient)
	hub := sse.NewHub()

	poolID := os.Getenv("POOL_ID")
	if poolID == "" {
		poolID = "main"
	}

	if isLambda() {
		lambda.Start(func(ctx context.Context) error {
			return runSync(ctx, s, hub, poolID)
		})
	} else {
		interval := parseDuration(os.Getenv("SYNC_INTERVAL"), 60*time.Second)
		log.Printf("starting local cron: pool=%s interval=%s", poolID, interval)

		// Run once immediately
		if err := runSync(ctx, s, hub, poolID); err != nil {
			log.Printf("sync error: %v", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := runSync(ctx, s, hub, poolID); err != nil {
					log.Printf("sync error: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}
}

func runSync(ctx context.Context, s *syncer.Syncer, hub *sse.Hub, poolID string) error {
	log.Printf("syncing pool %s...", poolID)
	if err := s.Sync(ctx, poolID); err != nil {
		return err
	}
	log.Printf("sync complete for pool %s, broadcasting", poolID)
	hub.Broadcast("sync")
	return nil
}

func region() string {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r
	}
	return "us-east-1"
}

func isLambda() bool {
	return os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != ""
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("invalid SYNC_INTERVAL %q, using default %s", s, fallback)
		return fallback
	}
	return d
}
