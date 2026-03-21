package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/grocky/squares/internal/api"
	dynrepo "github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/sse"
	"github.com/grocky/squares/internal/syncer"
	"github.com/grocky/squares/web"
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

	hub := sse.NewHub()
	s := syncer.New(repo, espnClient)
	if os.Getenv("ADMIN_TOKEN") == "" {
		log.Println("WARNING: ADMIN_TOKEN not set — admin area is unprotected (dev mode)")
	}

	handler := api.NewHandler(repo, espnClient, web.FS, s, hub)
	mux := handler.Routes()

	// Serve static files
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		log.Fatalf("failed to create static FS: %v", err)
	}
	mux.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	if isLambda() {
		adapter := chiadapter.NewV2(mux)
		lambda.Start(adapter.ProxyWithContextV2)
	} else {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		log.Printf("listening on :%s", port)
		log.Fatal(http.ListenAndServe(":"+port, mux))
	}
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
