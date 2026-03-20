# =============================================================================
# API Gateway v2 (HTTP API) — proxies all requests to the server Lambda
# =============================================================================

resource "aws_apigatewayv2_api" "server" {
  name          = "squares"
  protocol_type = "HTTP"

  tags = {
    Name        = "squares"
    Application = "squares"
  }
}

resource "aws_apigatewayv2_integration" "server" {
  api_id                 = aws_apigatewayv2_api.server.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.server.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "default" {
  api_id    = aws_apigatewayv2_api.server.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.server.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.server.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway.arn
    format = jsonencode({
      requestId          = "$context.requestId"
      ip                 = "$context.identity.sourceIp"
      requestTime        = "$context.requestTime"
      httpMethod         = "$context.httpMethod"
      routeKey           = "$context.routeKey"
      status             = "$context.status"
      responseLength     = "$context.responseLength"
      integrationLatency = "$context.integrationLatency"
    })
  }

  tags = {
    Name        = "squares-stage"
    Application = "squares"
  }
}

# =============================================================================
# Custom Domain — squares.rockygray.com
# =============================================================================

resource "aws_apigatewayv2_domain_name" "squares" {
  domain_name = var.domain_name

  domain_name_configuration {
    certificate_arn = data.aws_acm_certificate.wildcard.arn
    endpoint_type   = "REGIONAL"
    security_policy = "TLS_1_2"
  }

  tags = {
    Name        = "squares-domain"
    Application = "squares"
  }
}

resource "aws_apigatewayv2_api_mapping" "squares" {
  api_id      = aws_apigatewayv2_api.server.id
  domain_name = aws_apigatewayv2_domain_name.squares.id
  stage       = aws_apigatewayv2_stage.default.id
}
