# =============================================================================
# DynamoDB Table
# =============================================================================

resource "aws_dynamodb_table" "squares" {
  name         = "squares"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  tags = {
    Name        = "squares"
    Application = "squares"
  }
}
