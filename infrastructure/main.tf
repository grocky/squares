terraform {
  required_version = ">= 1.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  backend "s3" {
    bucket       = "grocky-tfstate"
    key          = "squares.rockygray.com/terraform.tfstate"
    region       = "us-east-1"
    use_lockfile = true
    encrypt      = true
  }
}

provider "aws" {
  region = "us-east-1"
}

# =============================================================================
# Variables
# =============================================================================

variable "domain_name" {
  description = "Domain for the squares app"
  type        = string
  default     = "squares.rockygray.com"
}

variable "pool_id" {
  description = "Default pool ID"
  type        = string
  default     = "main"
}

variable "sync_interval_minutes" {
  description = "How often the score sync cron runs (in minutes)"
  type        = number
  default     = 5
}

# =============================================================================
# Data Sources
# =============================================================================

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# Root domain state (zone ID, cert)
data "terraform_remote_state" "rockygray_com" {
  backend = "s3"
  config = {
    bucket = "grocky-tfstate"
    region = "us-east-1"
    key    = "rockygray.com/terraform.tfstate"
  }
}

# ACM wildcard cert (must be in us-east-1 for CloudFront — already is)
data "aws_acm_certificate" "wildcard" {
  domain   = "*.rockygray.com"
  statuses = ["ISSUED"]
}

# Route53 zone
data "aws_route53_zone" "root" {
  name = "rockygray.com."
}

# SSM parameters
data "aws_ssm_parameter" "admin_token" {
  name = "/squares/admin-token"
}
