# =============================================================================
# CloudWatch Log Groups
# =============================================================================

resource "aws_cloudwatch_log_group" "cron" {
  name              = "/aws/lambda/squares-cron"
  retention_in_days = 14
}

# =============================================================================
# Cron Lambda (score sync) — stateless, no SSE needed
# =============================================================================

resource "aws_lambda_function" "cron" {
  function_name = "squares-cron"
  role          = aws_iam_role.lambda.arn
  handler       = "bootstrap"
  runtime       = "provided.al2023"
  architectures = ["arm64"]

  filename         = "${path.module}/../dist/bootstrap-cron.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/bootstrap-cron.zip")

  memory_size = 128
  timeout     = 60

  environment {
    variables = {
      DYNAMODB_TABLE = aws_dynamodb_table.squares.name
      POOL_ID        = var.pool_id
    }
  }

  tags = {
    Name        = "squares-cron"
    Application = "squares"
  }

  depends_on = [aws_cloudwatch_log_group.cron]
}

resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.cron.function_name
  principal     = "scheduler.amazonaws.com"
  source_arn    = aws_scheduler_schedule.sync.arn
}

# =============================================================================
# EventBridge Scheduler — score sync cron
# =============================================================================

resource "aws_iam_role" "scheduler" {
  name = "squares-scheduler"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "scheduler.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "scheduler_invoke" {
  role = aws_iam_role.scheduler.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.cron.arn
    }]
  })
}

resource "aws_scheduler_schedule" "sync" {
  name       = "squares-score-sync"
  group_name = "default"

  flexible_time_window {
    mode = "OFF"
  }

  schedule_expression = "rate(${var.sync_interval_minutes} minutes)"

  target {
    arn      = aws_lambda_function.cron.arn
    role_arn = aws_iam_role.scheduler.arn
  }
}
