# =============================================================================
# EC2 — t4g.micro (arm64, free-tier eligible first 12 months)
#
# Architecture:
#   Caddy (port 80/443, auto TLS via Let's Encrypt)
#     → reverse proxy → squares server (port 8080, localhost only)
#
# SSE notes:
#   - Caddy is SSE-friendly out of the box (no idle timeout like an ALB)
#   - The sync watcher goroutine polls DynamoDB and broadcasts via in-process hub
#   - No load balancer means no sticky-session concern (single instance)
# =============================================================================

# Latest Amazon Linux 2023 arm64 AMI
data "aws_ami" "al2023_arm64" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-arm64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "architecture"
    values = ["arm64"]
  }
}

resource "aws_key_pair" "deploy" {
  key_name   = "squares-deploy"
  public_key = var.ssh_public_key

  tags = {
    Application = "squares"
  }
}

resource "aws_instance" "server" {
  ami                    = data.aws_ami.al2023_arm64.id
  instance_type          = "t4g.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.ec2.id]
  iam_instance_profile   = aws_iam_instance_profile.ec2.name
  key_name               = aws_key_pair.deploy.key_name

  root_block_device {
    volume_size = 30
    volume_type = "gp3"
  }

  user_data = templatefile("${path.module}/userdata.sh", {
    domain           = var.domain_name
    admin_token_path = "/squares/admin-token"
    pool_id          = var.pool_id
    dynamodb_table   = aws_dynamodb_table.squares.name
    aws_region       = data.aws_region.current.id
    caddy_version    = "2.9.1"
  })

  # Replace instance (not in-place update) when user_data changes,
  # since AL2023 user_data only runs once at first boot.
  lifecycle {
    create_before_destroy = true
  }

  tags = {
    Name        = "squares-server"
    Application = "squares"
  }
}

# Elastic IP — stable address for DNS; survives stop/start cycles
resource "aws_eip" "server" {
  instance = aws_instance.server.id
  domain   = "vpc"

  tags = {
    Name        = "squares-server"
    Application = "squares"
  }
}
