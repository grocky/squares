package dynamo

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/grocky/squares/internal/models"
)

type Repo struct {
	client    *dynamodb.Client
	tableName string
}

func NewRepo(client *dynamodb.Client) *Repo {
	table := os.Getenv("DYNAMODB_TABLE")
	if table == "" {
		table = "squares"
	}
	return &Repo{client: client, tableName: table}
}

// Pool operations

func (r *Repo) PutPool(ctx context.Context, pool models.Pool) error {
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":        &types.AttributeValueMemberS{Value: "POOL#" + pool.ID},
			"SK":        &types.AttributeValueMemberS{Value: "METADATA"},
			"name":      &types.AttributeValueMemberS{Value: pool.Name},
			"status":    &types.AttributeValueMemberS{Value: pool.Status},
			"createdAt": &types.AttributeValueMemberS{Value: pool.CreatedAt.Format(time.RFC3339)},
		},
	})
	return err
}

func (r *Repo) GetPool(ctx context.Context, poolID string) (models.Pool, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.tableName,
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK": &types.AttributeValueMemberS{Value: "METADATA"},
		},
	})
	if err != nil {
		return models.Pool{}, err
	}
	if out.Item == nil {
		return models.Pool{}, fmt.Errorf("pool %q not found", poolID)
	}
	return poolFromItem(out.Item, poolID)
}

func poolFromItem(item map[string]types.AttributeValue, poolID string) (models.Pool, error) {
	var pool models.Pool
	pool.ID = poolID
	if v, ok := item["name"].(*types.AttributeValueMemberS); ok {
		pool.Name = v.Value
	}
	if v, ok := item["status"].(*types.AttributeValueMemberS); ok {
		pool.Status = v.Value
	}
	if v, ok := item["createdAt"].(*types.AttributeValueMemberS); ok {
		pool.CreatedAt, _ = time.Parse(time.RFC3339, v.Value)
	}
	return pool, nil
}

// RoundConfig operations

func (r *Repo) PutRoundConfig(ctx context.Context, rc models.RoundConfig) error {
	sk := fmt.Sprintf("ROUND#%d#CONFIG", rc.RoundNum)
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":           &types.AttributeValueMemberS{Value: "POOL#" + rc.PoolID},
			"SK":           &types.AttributeValueMemberS{Value: sk},
			"roundNum":     &types.AttributeValueMemberN{Value: strconv.Itoa(rc.RoundNum)},
			"name":         &types.AttributeValueMemberS{Value: rc.Name},
			"payoutAmount": &types.AttributeValueMemberN{Value: fmt.Sprintf("%.2f", rc.PayoutAmount)},
		},
	})
	return err
}

func (r *Repo) GetRoundConfig(ctx context.Context, poolID string, roundNum int) (models.RoundConfig, error) {
	sk := fmt.Sprintf("ROUND#%d#CONFIG", roundNum)
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.tableName,
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return models.RoundConfig{}, err
	}
	if out.Item == nil {
		return models.RoundConfig{}, fmt.Errorf("round config %d for pool %q not found", roundNum, poolID)
	}
	return roundConfigFromItem(out.Item, poolID)
}

func (r *Repo) GetAllRoundConfigs(ctx context.Context, poolID string) ([]models.RoundConfig, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.tableName,
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			":prefix": &types.AttributeValueMemberS{Value: "ROUND#"},
		},
	})
	if err != nil {
		return nil, err
	}
	var configs []models.RoundConfig
	for _, item := range out.Items {
		// Only match CONFIG items (skip AXIS items which also start with ROUND#)
		if sk, ok := item["SK"].(*types.AttributeValueMemberS); ok {
			if !strings.HasSuffix(sk.Value, "#CONFIG") {
				continue
			}
		}
		rc, err := roundConfigFromItem(item, poolID)
		if err != nil {
			return nil, err
		}
		configs = append(configs, rc)
	}
	return configs, nil
}

func roundConfigFromItem(item map[string]types.AttributeValue, poolID string) (models.RoundConfig, error) {
	rc := models.RoundConfig{PoolID: poolID}
	if v, ok := item["roundNum"].(*types.AttributeValueMemberN); ok {
		rc.RoundNum, _ = strconv.Atoi(v.Value)
	}
	if v, ok := item["name"].(*types.AttributeValueMemberS); ok {
		rc.Name = v.Value
	}
	if v, ok := item["payoutAmount"].(*types.AttributeValueMemberN); ok {
		rc.PayoutAmount, _ = strconv.ParseFloat(v.Value, 64)
	}
	return rc, nil
}

// Axis operations (per-round)

func (r *Repo) PutRoundAxis(ctx context.Context, axis models.Axis) error {
	digitStrs := make([]types.AttributeValue, len(axis.Digits))
	for i, d := range axis.Digits {
		digitStrs[i] = &types.AttributeValueMemberN{Value: strconv.Itoa(d)}
	}
	sk := fmt.Sprintf("ROUND#%d#AXIS#%s", axis.RoundNum, axis.Type)
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":     &types.AttributeValueMemberS{Value: "POOL#" + axis.PoolID},
			"SK":     &types.AttributeValueMemberS{Value: sk},
			"digits": &types.AttributeValueMemberL{Value: digitStrs},
		},
	})
	return err
}

func (r *Repo) GetRoundAxis(ctx context.Context, poolID string, roundNum int, axisType string) (models.Axis, error) {
	sk := fmt.Sprintf("ROUND#%d#AXIS#%s", roundNum, axisType)
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.tableName,
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return models.Axis{}, err
	}
	if out.Item == nil {
		return models.Axis{}, fmt.Errorf("axis round=%d type=%s for pool %q not found", roundNum, axisType, poolID)
	}
	return axisFromItem(out.Item, poolID, roundNum, axisType)
}

func axisFromItem(item map[string]types.AttributeValue, poolID string, roundNum int, axisType string) (models.Axis, error) {
	axis := models.Axis{PoolID: poolID, RoundNum: roundNum, Type: axisType}
	if v, ok := item["digits"].(*types.AttributeValueMemberL); ok {
		for _, d := range v.Value {
			if n, ok := d.(*types.AttributeValueMemberN); ok {
				val, _ := strconv.Atoi(n.Value)
				axis.Digits = append(axis.Digits, val)
			}
		}
	}
	return axis, nil
}

// Square operations

func (r *Repo) PutSquare(ctx context.Context, sq models.Square) error {
	sk := fmt.Sprintf("SQUARE#%d%d", sq.Row, sq.Col)
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":        &types.AttributeValueMemberS{Value: "POOL#" + sq.PoolID},
			"SK":        &types.AttributeValueMemberS{Value: sk},
			"ownerName": &types.AttributeValueMemberS{Value: sq.OwnerName},
			"row":       &types.AttributeValueMemberN{Value: strconv.Itoa(sq.Row)},
			"col":       &types.AttributeValueMemberN{Value: strconv.Itoa(sq.Col)},
		},
	})
	return err
}

func (r *Repo) GetAllSquares(ctx context.Context, poolID string) ([]models.Square, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.tableName,
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			":prefix": &types.AttributeValueMemberS{Value: "SQUARE#"},
		},
	})
	if err != nil {
		return nil, err
	}
	var squares []models.Square
	for _, item := range out.Items {
		sq := models.Square{PoolID: poolID}
		if v, ok := item["ownerName"].(*types.AttributeValueMemberS); ok {
			sq.OwnerName = v.Value
		}
		if v, ok := item["row"].(*types.AttributeValueMemberN); ok {
			sq.Row, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["col"].(*types.AttributeValueMemberN); ok {
			sq.Col, _ = strconv.Atoi(v.Value)
		}
		squares = append(squares, sq)
	}
	return squares, nil
}

// Game operations

func (r *Repo) PutGame(ctx context.Context, game models.Game) error {
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":          &types.AttributeValueMemberS{Value: "POOL#" + game.PoolID},
			"SK":          &types.AttributeValueMemberS{Value: "GAME#" + game.EspnID},
			"homeTeam":    &types.AttributeValueMemberS{Value: game.HomeTeam},
			"awayTeam":    &types.AttributeValueMemberS{Value: game.AwayTeam},
			"round":       &types.AttributeValueMemberS{Value: game.Round},
			"roundNum":    &types.AttributeValueMemberN{Value: strconv.Itoa(game.RoundNum)},
			"homeScore":   &types.AttributeValueMemberN{Value: strconv.Itoa(game.HomeScore)},
			"awayScore":   &types.AttributeValueMemberN{Value: strconv.Itoa(game.AwayScore)},
			"winnerScore": &types.AttributeValueMemberN{Value: strconv.Itoa(game.WinnerScore)},
			"loserScore":  &types.AttributeValueMemberN{Value: strconv.Itoa(game.LoserScore)},
			"status":      &types.AttributeValueMemberS{Value: game.Status},
			"startTime":   &types.AttributeValueMemberS{Value: game.StartTime.Format(time.RFC3339)},
			"syncedAt":    &types.AttributeValueMemberS{Value: game.SyncedAt.Format(time.RFC3339)},
		},
	})
	return err
}

func (r *Repo) GetAllGames(ctx context.Context, poolID string) ([]models.Game, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.tableName,
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			":prefix": &types.AttributeValueMemberS{Value: "GAME#"},
		},
	})
	if err != nil {
		return nil, err
	}
	var games []models.Game
	for _, item := range out.Items {
		g := models.Game{PoolID: poolID}
		if v, ok := item["SK"].(*types.AttributeValueMemberS); ok {
			g.EspnID = strings.TrimPrefix(v.Value, "GAME#")
		}
		if v, ok := item["homeTeam"].(*types.AttributeValueMemberS); ok {
			g.HomeTeam = v.Value
		}
		if v, ok := item["awayTeam"].(*types.AttributeValueMemberS); ok {
			g.AwayTeam = v.Value
		}
		if v, ok := item["round"].(*types.AttributeValueMemberS); ok {
			g.Round = v.Value
		}
		if v, ok := item["roundNum"].(*types.AttributeValueMemberN); ok {
			g.RoundNum, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["homeScore"].(*types.AttributeValueMemberN); ok {
			g.HomeScore, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["awayScore"].(*types.AttributeValueMemberN); ok {
			g.AwayScore, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["winnerScore"].(*types.AttributeValueMemberN); ok {
			g.WinnerScore, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["loserScore"].(*types.AttributeValueMemberN); ok {
			g.LoserScore, _ = strconv.Atoi(v.Value)
		}
		if v, ok := item["status"].(*types.AttributeValueMemberS); ok {
			g.Status = v.Value
		}
		if v, ok := item["startTime"].(*types.AttributeValueMemberS); ok {
			g.StartTime, _ = time.Parse(time.RFC3339, v.Value)
		}
		if v, ok := item["syncedAt"].(*types.AttributeValueMemberS); ok {
			g.SyncedAt, _ = time.Parse(time.RFC3339, v.Value)
		}
		games = append(games, g)
	}
	return games, nil
}

// Payout operations

func (r *Repo) PutPayout(ctx context.Context, p models.Payout) error {
	sk := fmt.Sprintf("PAYOUT#%s#%d%d", p.GameID, p.Row, p.Col)
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":          &types.AttributeValueMemberS{Value: "POOL#" + p.PoolID},
			"SK":          &types.AttributeValueMemberS{Value: sk},
			"ownerName":   &types.AttributeValueMemberS{Value: p.OwnerName},
			"row":         &types.AttributeValueMemberN{Value: strconv.Itoa(p.Row)},
			"col":         &types.AttributeValueMemberN{Value: strconv.Itoa(p.Col)},
			"winnerScore": &types.AttributeValueMemberN{Value: strconv.Itoa(p.WinnerScore)},
			"loserScore":  &types.AttributeValueMemberN{Value: strconv.Itoa(p.LoserScore)},
			"amount":      &types.AttributeValueMemberN{Value: fmt.Sprintf("%.2f", p.Amount)},
			"gameID":      &types.AttributeValueMemberS{Value: p.GameID},
		},
	})
	return err
}

func (r *Repo) GetAllPayouts(ctx context.Context, poolID string) ([]models.Payout, error) {
	out, err := r.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.tableName,
		KeyConditionExpression: aws.String("PK = :pk AND begins_with(SK, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			":prefix": &types.AttributeValueMemberS{Value: "PAYOUT#"},
		},
	})
	if err != nil {
		return nil, err
	}
	var payouts []models.Payout
	for _, item := range out.Items {
		var p models.Payout
		if err := attributevalue.UnmarshalMap(item, &p); err != nil {
			p = payoutFromItem(item, poolID)
		} else {
			p.PoolID = poolID
			p = payoutFromItem(item, poolID)
		}
		payouts = append(payouts, p)
	}
	return payouts, nil
}

func payoutFromItem(item map[string]types.AttributeValue, poolID string) models.Payout {
	p := models.Payout{PoolID: poolID}
	if v, ok := item["ownerName"].(*types.AttributeValueMemberS); ok {
		p.OwnerName = v.Value
	}
	if v, ok := item["row"].(*types.AttributeValueMemberN); ok {
		p.Row, _ = strconv.Atoi(v.Value)
	}
	if v, ok := item["col"].(*types.AttributeValueMemberN); ok {
		p.Col, _ = strconv.Atoi(v.Value)
	}
	if v, ok := item["winnerScore"].(*types.AttributeValueMemberN); ok {
		p.WinnerScore, _ = strconv.Atoi(v.Value)
	}
	if v, ok := item["loserScore"].(*types.AttributeValueMemberN); ok {
		p.LoserScore, _ = strconv.Atoi(v.Value)
	}
	if v, ok := item["amount"].(*types.AttributeValueMemberN); ok {
		p.Amount, _ = strconv.ParseFloat(v.Value, 64)
	}
	if v, ok := item["gameID"].(*types.AttributeValueMemberS); ok {
		p.GameID = v.Value
	}
	return p
}

// SyncState tracks the last time the cron wrote a sync, so the server can
// detect changes via polling instead of requiring an inbound HTTP call.

func (r *Repo) PutSyncState(ctx context.Context, poolID string, syncedAt time.Time) error {
	_, err := r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.tableName,
		Item: map[string]types.AttributeValue{
			"PK":       &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK":       &types.AttributeValueMemberS{Value: "SYNC_STATE"},
			"syncedAt": &types.AttributeValueMemberS{Value: syncedAt.UTC().Format(time.RFC3339)},
		},
	})
	return err
}

func (r *Repo) GetSyncState(ctx context.Context, poolID string) (time.Time, error) {
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.tableName,
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK": &types.AttributeValueMemberS{Value: "SYNC_STATE"},
		},
	})
	if err != nil {
		return time.Time{}, err
	}
	if out.Item == nil {
		return time.Time{}, nil
	}
	if v, ok := out.Item["syncedAt"].(*types.AttributeValueMemberS); ok {
		return time.Parse(time.RFC3339, v.Value)
	}
	return time.Time{}, nil
}

func (r *Repo) PayoutExists(ctx context.Context, poolID, gameID string, row, col int) (bool, error) {
	sk := fmt.Sprintf("PAYOUT#%s#%d%d", gameID, row, col)
	out, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.tableName,
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "POOL#" + poolID},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
	})
	if err != nil {
		return false, err
	}
	return out.Item != nil, nil
}
