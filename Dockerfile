FROM golang:1.26-alpine AS builder
RUN apk --no-cache add git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Inject git SHA at build time; falls back to "unknown" if not in a git repo
RUN GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown") && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
      -ldflags "-X github.com/grocky/squares/internal/version.commit=${GIT_COMMIT}" \
      -o server ./cmd/server

FROM alpine:3.20
RUN apk --no-cache add ca-certificates curl
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
