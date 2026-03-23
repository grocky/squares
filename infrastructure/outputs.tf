output "url" {
  value = "https://${var.domain_name}"
}

output "ec2_public_ip" {
  value = aws_eip.server.public_ip
}

output "ec2_instance_id" {
  value = aws_instance.server.id
}

output "dynamodb_table" {
  value = aws_dynamodb_table.squares.name
}

output "cron_lambda" {
  value = aws_lambda_function.cron.function_name
}

output "ssh_connect" {
  value = "ssh -i ~/.ssh/squares ec2-user@${aws_eip.server.public_ip}"
}
