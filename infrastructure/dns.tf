# =============================================================================
# DNS — squares.rockygray.com → Elastic IP
# =============================================================================

resource "aws_route53_record" "squares" {
  zone_id = data.aws_route53_zone.root.zone_id
  name    = var.domain_name
  type    = "A"
  ttl     = 300
  records = [aws_eip.server.public_ip]
}
