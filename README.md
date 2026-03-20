# NCAA Basketball Tournament Squares

A web app for tracking a "squares" pool for the NCAA Basketball Tournament. Each square in a 10x10 grid is owned by a person. Rows and columns represent the last digit of home and away scores. When a game ends, the matching square wins a payout.

## Requirements

- Go 1.21+
- AWS credentials configured (for DynamoDB access)
- DynamoDB table named "squares" with PK (String) and SK (String) keys

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `DYNAMODB_TABLE` | `squares` | DynamoDB table name |
| `AWS_REGION` | `us-east-1` | AWS region |
| `PORT` | `8080` | HTTP server port |

## Running Locally

```bash
# Seed the database with sample data
make seed

# Start the server
make run

# Sync ESPN scores
make sync
```

## Building for Lambda

```bash
make build
# Produces a `bootstrap` binary for Lambda (linux/arm64)
```

## How It Works

1. Create a pool with a payout amount per game
2. Assign row/col axes (random digits 0-9)
3. Assign owners to each of the 100 squares
4. Sync live scores from ESPN's NCAA basketball API
5. When a game ends, the square at (home_score % 10, away_score % 10) wins the payout
