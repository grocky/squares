# =============================================================================
# IAM — shared DynamoDB policy + EC2 instance role + Lambda execution role
# =============================================================================

# ---------------------------------------------------------------------------
# Shared DynamoDB policy (used by EC2 instance + cron Lambda)
# ---------------------------------------------------------------------------
data "aws_iam_policy_document" "dynamodb" {
  statement {
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:DeleteItem",
      "dynamodb:Query",
      "dynamodb:Scan",
    ]
    resources = [aws_dynamodb_table.squares.arn]
  }
}

resource "aws_iam_policy" "dynamodb" {
  name   = "squares-dynamodb"
  policy = data.aws_iam_policy_document.dynamodb.json
}

# ---------------------------------------------------------------------------
# EC2 instance role — DynamoDB + SSM (to read ADMIN_TOKEN at startup)
# ---------------------------------------------------------------------------
resource "aws_iam_role" "ec2" {
  name = "squares-ec2"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })

  tags = {
    Application = "squares"
  }
}

resource "aws_iam_role_policy_attachment" "ec2_dynamodb" {
  role       = aws_iam_role.ec2.name
  policy_arn = aws_iam_policy.dynamodb.arn
}

resource "aws_iam_role_policy" "ec2_ssm" {
  role = aws_iam_role.ec2.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameter", "ssm:GetParameters"]
      Resource = "arn:aws:ssm:${data.aws_region.current.id}:${data.aws_caller_identity.current.account_id}:parameter/squares/*"
    }]
  })
}

resource "aws_iam_instance_profile" "ec2" {
  name = "squares-ec2"
  role = aws_iam_role.ec2.name
}

# ---------------------------------------------------------------------------
# Lambda execution role (cron only)
# ---------------------------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name               = "squares-lambda"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json

  tags = {
    Application = "squares"
  }
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy_attachment" "lambda_dynamodb" {
  role       = aws_iam_role.lambda.name
  policy_arn = aws_iam_policy.dynamodb.arn
}
