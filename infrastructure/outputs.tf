output "url" {
  value = "https://${var.domain_name}"
}

output "alb_dns" {
  value = aws_lb.main.dns_name
}

output "ecr_repository_url" {
  value = aws_ecr_repository.server.repository_url
}

output "dynamodb_table" {
  value = aws_dynamodb_table.squares.name
}

output "ecs_cluster" {
  value = aws_ecs_cluster.main.name
}

output "cron_lambda" {
  value = aws_lambda_function.cron.function_name
}
