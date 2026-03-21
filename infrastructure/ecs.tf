# =============================================================================
# ECS Fargate — persistent server process (supports SSE)
# =============================================================================

resource "aws_ecs_cluster" "main" {
  name = "squares"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = {
    Name        = "squares"
    Application = "squares"
  }
}

# IAM role for ECS task execution (pull images, write logs)
resource "aws_iam_role" "ecs_execution" {
  name = "squares-ecs-execution"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_execution" {
  role       = aws_iam_role.ecs_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Allow ECS execution role to read SSM parameters (for ADMIN_TOKEN)
resource "aws_iam_role_policy" "ecs_execution_ssm" {
  role = aws_iam_role.ecs_execution.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameter", "ssm:GetParameters"]
      Resource = "arn:aws:ssm:${data.aws_region.current.id}:${data.aws_caller_identity.current.account_id}:parameter/squares/*"
    }]
  })
}

# IAM role for the task itself (DynamoDB access)
resource "aws_iam_role" "ecs_task" {
  name = "squares-ecs-task"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecs_task_dynamodb" {
  role       = aws_iam_role.ecs_task.name
  policy_arn = aws_iam_policy.dynamodb.arn
}

# CloudWatch log group
resource "aws_cloudwatch_log_group" "ecs" {
  name              = "/ecs/squares-server"
  retention_in_days = 14
}

# Task definition
resource "aws_ecs_task_definition" "server" {
  family                   = "squares-server"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_execution.arn
  task_role_arn            = aws_iam_role.ecs_task.arn

  container_definitions = jsonencode([{
    name      = "server"
    image     = "${aws_ecr_repository.server.repository_url}:latest"
    essential = true

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = [
      { name = "DYNAMODB_TABLE", value = aws_dynamodb_table.squares.name },
      { name = "PORT", value = "8080" },
      { name = "POOL_ID", value = var.pool_id },
    ]

    secrets = [
      {
        name      = "ADMIN_TOKEN"
        valueFrom = data.aws_ssm_parameter.admin_token.arn
      }
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        awslogs-group         = aws_cloudwatch_log_group.ecs.name
        awslogs-region        = data.aws_region.current.id
        awslogs-stream-prefix = "server"
      }
    }

    healthCheck = {
      command     = ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 10
    }
  }])

  tags = {
    Name        = "squares-server"
    Application = "squares"
  }
}

# ECS Service
resource "aws_ecs_service" "server" {
  name            = "squares-server"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.server.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  # Rolling deploy: bring up new task before stopping old one
  deployment_minimum_healthy_percent = 100
  deployment_maximum_percent         = 200

  # Auto-rollback if the new task fails health checks
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.fargate.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.server.arn
    container_name   = "server"
    container_port   = 8080
  }

  depends_on = [aws_lb_listener.https]

  tags = {
    Name        = "squares"
    Application = "squares"
  }
}
