.PHONY: run build seed sync

run:
	go run ./cmd/server

build:
	GOOS=linux GOARCH=arm64 go build -o bootstrap ./cmd/server

seed:
	go run ./cmd/seed

sync:
	curl -X POST http://localhost:8080/pools/main/sync
