# =============================================================================
# DNS — squares.rockygray.com → ALB
# =============================================================================

resource "aws_route53_record" "squares" {
  zone_id = data.terraform_remote_state.rockygray_com.outputs.root_domain.root_zone_id
  name    = var.domain_name
  type    = "A"

  alias {
    name                   = aws_lb.main.dns_name
    zone_id                = aws_lb.main.zone_id
    evaluate_target_health = true
  }
}
