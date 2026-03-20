output "url" {
  value = "https://${var.domain_name}"
}

output "api_gateway_url" {
  value = aws_apigatewayv2_stage.default.invoke_url
}

output "dynamodb_table" {
  value = aws_dynamodb_table.squares.name
}

output "server_lambda" {
  value = aws_lambda_function.server.function_name
}

output "cron_lambda" {
  value = aws_lambda_function.cron.function_name
}
