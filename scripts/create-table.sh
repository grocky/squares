#!/usr/bin/env bash
set -e

ENDPOINT=${DYNAMODB_ENDPOINT:-http://localhost:8000}
TABLE=${DYNAMODB_TABLE:-squares}
REGION=${AWS_REGION:-us-east-1}

echo "Creating DynamoDB table '$TABLE' at $ENDPOINT..."

aws dynamodb create-table \
  --table-name "$TABLE" \
  --attribute-definitions \
    AttributeName=PK,AttributeType=S \
    AttributeName=SK,AttributeType=S \
  --key-schema \
    AttributeName=PK,KeyType=HASH \
    AttributeName=SK,KeyType=RANGE \
  --billing-mode PAY_PER_REQUEST \
  --endpoint-url "$ENDPOINT" \
  --region "$REGION" \
  --output json > /dev/null

echo "Table '$TABLE' created."
