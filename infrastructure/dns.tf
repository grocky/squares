# =============================================================================
# DNS — squares.rockygray.com → API Gateway custom domain
# =============================================================================

resource "aws_route53_record" "squares" {
  zone_id = data.terraform_remote_state.rockygray_com.outputs.root_domain.root_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_apigatewayv2_domain_name.squares.domain_name_configuration[0].target_domain_name
    zone_id                = aws_apigatewayv2_domain_name.squares.domain_name_configuration[0].hosted_zone_id
    evaluate_target_health = false
  }
}
