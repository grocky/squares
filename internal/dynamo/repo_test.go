package dynamo

import (
	"context"
	"strconv"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/grocky/squares/internal/models"
)

// mockDynamoClient implements just enough of the DynamoDB client interface for testing.
type mockDynamoClient struct {
	putItemFunc func(ctx context.Context, input *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	queryFunc   func(ctx context.Context, input *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	getItemFunc func(ctx context.Context, input *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
}

func (m *mockDynamoClient) PutItem(ctx context.Context, input *dynamodb.PutItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return m.putItemFunc(ctx, input, opts...)
}

func (m *mockDynamoClient) Query(ctx context.Context, input *dynamodb.QueryInput, opts ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	return m.queryFunc(ctx, input, opts...)
}

func (m *mockDynamoClient) GetItem(ctx context.Context, input *dynamodb.GetItemInput, opts ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return m.getItemFunc(ctx, input, opts...)
}

func TestPutGameGlobal_WritesPKGAMES(t *testing.T) {
	var capturedInput *dynamodb.PutItemInput
	mock := &mockDynamoClient{
		putItemFunc: func(_ context.Context, input *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
			capturedInput = input
			return &dynamodb.PutItemOutput{}, nil
		},
	}

	repo := &Repo{client: mock, tableName: "test-table"}
	game := models.Game{
		EspnID:   "401234",
		HomeTeam: "Duke",
		AwayTeam: "UNC",
		Status:   "final",
		RoundNum: 1,
	}

	err := repo.PutGameGlobal(context.Background(), game)
	if err != nil {
		t.Fatalf("PutGameGlobal error: %v", err)
	}

	pk, ok := capturedInput.Item["PK"].(*types.AttributeValueMemberS)
	if !ok || pk.Value != "GAMES" {
		t.Errorf("PK = %v, want GAMES", capturedInput.Item["PK"])
	}
	sk, ok := capturedInput.Item["SK"].(*types.AttributeValueMemberS)
	if !ok || sk.Value != "GAME#401234" {
		t.Errorf("SK = %v, want GAME#401234", capturedInput.Item["SK"])
	}
	if *capturedInput.TableName != "test-table" {
		t.Errorf("table = %q, want test-table", *capturedInput.TableName)
	}
}

func TestGetAllGamesGlobal_ReturnsGamesFromGlobalPartition(t *testing.T) {
	mock := &mockDynamoClient{
		queryFunc: func(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			// Verify query targets PK=GAMES
			pk := input.ExpressionAttributeValues[":pk"].(*types.AttributeValueMemberS)
			if pk.Value != "GAMES" {
				t.Errorf("query PK = %q, want GAMES", pk.Value)
			}
			return &dynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{
					{
						"SK":          &types.AttributeValueMemberS{Value: "GAME#401"},
						"homeTeam":    &types.AttributeValueMemberS{Value: "Duke"},
						"awayTeam":    &types.AttributeValueMemberS{Value: "UNC"},
						"status":      &types.AttributeValueMemberS{Value: "final"},
						"roundNum":    &types.AttributeValueMemberN{Value: "1"},
						"homeScore":   &types.AttributeValueMemberN{Value: "75"},
						"awayScore":   &types.AttributeValueMemberN{Value: "68"},
						"winnerScore": &types.AttributeValueMemberN{Value: "75"},
						"loserScore":  &types.AttributeValueMemberN{Value: "68"},
						"round":       &types.AttributeValueMemberS{Value: ""},
						"startTime":   &types.AttributeValueMemberS{Value: "0001-01-01T00:00:00Z"},
						"syncedAt":    &types.AttributeValueMemberS{Value: "0001-01-01T00:00:00Z"},
					},
					{
						"SK":          &types.AttributeValueMemberS{Value: "GAME#402"},
						"homeTeam":    &types.AttributeValueMemberS{Value: "Kansas"},
						"awayTeam":    &types.AttributeValueMemberS{Value: "Kentucky"},
						"status":      &types.AttributeValueMemberS{Value: "in_progress"},
						"roundNum":    &types.AttributeValueMemberN{Value: "2"},
						"homeScore":   &types.AttributeValueMemberN{Value: "40"},
						"awayScore":   &types.AttributeValueMemberN{Value: "35"},
						"winnerScore": &types.AttributeValueMemberN{Value: "40"},
						"loserScore":  &types.AttributeValueMemberN{Value: "35"},
						"round":       &types.AttributeValueMemberS{Value: ""},
						"startTime":   &types.AttributeValueMemberS{Value: "0001-01-01T00:00:00Z"},
						"syncedAt":    &types.AttributeValueMemberS{Value: "0001-01-01T00:00:00Z"},
					},
				},
			}, nil
		},
	}

	repo := &Repo{client: mock, tableName: "test-table"}
	games, err := repo.GetAllGamesGlobal(context.Background())
	if err != nil {
		t.Fatalf("GetAllGamesGlobal error: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games, want 2", len(games))
	}
	if games[0].EspnID != "401" {
		t.Errorf("games[0].EspnID = %q, want 401", games[0].EspnID)
	}
	if games[0].HomeTeam != "Duke" {
		t.Errorf("games[0].HomeTeam = %q, want Duke", games[0].HomeTeam)
	}
	if games[1].EspnID != "402" {
		t.Errorf("games[1].EspnID = %q, want 402", games[1].EspnID)
	}
	if games[1].RoundNum != 2 {
		t.Errorf("games[1].RoundNum = %d, want 2", games[1].RoundNum)
	}
}

func TestGetAllGamesGlobal_EmptyResult(t *testing.T) {
	mock := &mockDynamoClient{
		queryFunc: func(_ context.Context, _ *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			return &dynamodb.QueryOutput{Items: nil}, nil
		},
	}

	repo := &Repo{client: mock, tableName: "test-table"}
	games, err := repo.GetAllGamesGlobal(context.Background())
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(games) != 0 {
		t.Errorf("got %d games, want 0", len(games))
	}
}

func TestGetAllRoundAxes_ReturnsAxesSkipsConfigs(t *testing.T) {
	mock := &mockDynamoClient{
		queryFunc: func(_ context.Context, input *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
			pk := input.ExpressionAttributeValues[":pk"].(*types.AttributeValueMemberS)
			if pk.Value != "POOL#p1" {
				t.Errorf("query PK = %q, want POOL#p1", pk.Value)
			}
			return &dynamodb.QueryOutput{
				Items: []map[string]types.AttributeValue{
					// CONFIG item — should be skipped
					{
						"SK":       &types.AttributeValueMemberS{Value: "ROUND#1#CONFIG"},
						"roundNum": &types.AttributeValueMemberN{Value: "1"},
					},
					// AXIS items — should be returned
					{
						"SK": &types.AttributeValueMemberS{Value: "ROUND#1#AXIS#winner"},
						"digits": &types.AttributeValueMemberL{Value: func() []types.AttributeValue {
							d := make([]types.AttributeValue, 10)
							for i := 0; i < 10; i++ {
								d[i] = &types.AttributeValueMemberN{Value: strconv.Itoa(i)}
							}
							return d
						}()},
					},
					{
						"SK": &types.AttributeValueMemberS{Value: "ROUND#1#AXIS#loser"},
						"digits": &types.AttributeValueMemberL{Value: func() []types.AttributeValue {
							d := make([]types.AttributeValue, 10)
							for i := 9; i >= 0; i-- {
								d[9-i] = &types.AttributeValueMemberN{Value: strconv.Itoa(i)}
							}
							return d
						}()},
					},
					{
						"SK": &types.AttributeValueMemberS{Value: "ROUND#2#AXIS#winner"},
						"digits": &types.AttributeValueMemberL{Value: func() []types.AttributeValue {
							d := make([]types.AttributeValue, 10)
							for i := 0; i < 10; i++ {
								d[i] = &types.AttributeValueMemberN{Value: strconv.Itoa(i)}
							}
							return d
						}()},
					},
				},
			}, nil
		},
	}

	repo := &Repo{client: mock, tableName: "test-table"}
	axes, err := repo.GetAllRoundAxes(context.Background(), "p1")
	if err != nil {
		t.Fatalf("GetAllRoundAxes error: %v", err)
	}
	if len(axes) != 3 {
		t.Fatalf("got %d axes, want 3 (config should be skipped)", len(axes))
	}

	// Verify first axis
	if axes[0].PoolID != "p1" {
		t.Errorf("axes[0].PoolID = %q, want p1", axes[0].PoolID)
	}
	if axes[0].RoundNum != 1 {
		t.Errorf("axes[0].RoundNum = %d, want 1", axes[0].RoundNum)
	}
	if axes[0].Type != "winner" {
		t.Errorf("axes[0].Type = %q, want winner", axes[0].Type)
	}
	if len(axes[0].Digits) != 10 {
		t.Errorf("axes[0].Digits len = %d, want 10", len(axes[0].Digits))
	}

	// Verify second axis is loser
	if axes[1].Type != "loser" {
		t.Errorf("axes[1].Type = %q, want loser", axes[1].Type)
	}

	// Verify third axis is round 2
	if axes[2].RoundNum != 2 {
		t.Errorf("axes[2].RoundNum = %d, want 2", axes[2].RoundNum)
	}
}
