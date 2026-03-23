package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
	"github.com/grocky/squares/internal/api"
	dynrepo "github.com/grocky/squares/internal/dynamo"
	"github.com/grocky/squares/internal/espn"
	"github.com/grocky/squares/internal/sse"
	"github.com/grocky/squares/internal/syncer"
	"github.com/grocky/squares/internal/version"
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

	appVersion := version.Get()

	poolID := os.Getenv("POOL_ID")
	if poolID == "" {
		poolID = "main"
	}

	handlerConfig := api.HandlerConfig{
		Version:    appVersion,
		Repo:       repo,
		EspnClient: espnClient,
		Syncer:     s,
		Hub:        hub,
		TemplateFS: web.FS,
	}

	handler := api.NewHandler(handlerConfig)
	mux := handler.Routes()

	// Serve static files
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		log.Fatalf("failed to create static FS: %v", err)
	}
	mux.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Serve vendor files
	vendorFS, err := fs.Sub(web.FS, "vendor")
	if err != nil {
		log.Fatalf("failed to create vendor FS: %v", err)
	}
	mux.Handle("/vendor/*", http.StripPrefix("/vendor/", http.FileServer(http.FS(vendorFS))))

	if isLambda() {
		adapter := chiadapter.NewV2(mux)
		lambda.Start(adapter.ProxyWithContextV2)
	} else {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}

		srv := &http.Server{
			Addr:    ":" + port,
			Handler: mux,
		}

		// Poll DynamoDB for sync state changes and broadcast SSE events.
		// This replaces the Lambda → HTTP broadcast call — no egress needed.
		watchCtx, watchCancel := context.WithCancel(ctx)
		go sse.WatchSyncState(watchCtx, repo, hub, poolID, 15*time.Second)

		// Start server in background
		go func() {
			log.Printf("listening on :%s, version: %s", port, appVersion)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		// Wait for SIGTERM or SIGINT (ECS sends SIGTERM on task stop)
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
		sig := <-quit
		log.Printf("received signal %s — shutting down gracefully", sig)

		// Stop the sync watcher before shutting down the server.
		watchCancel()

		// Notify SSE clients to reconnect quickly before we stop accepting
		// connections. This runs first so clients start reconnecting while
		// the new task is still in the ALB warmup window.
		hub.Shutdown()

		// Give in-flight requests up to 25s to finish.
		// ALB deregistration_delay is 30s, so the ALB stops sending new
		// requests before ECS kills us; 25s leaves a 5s safety margin.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown timed out: %v", err)
		} else {
			log.Println("server stopped cleanly")
		}
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
