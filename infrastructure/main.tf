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

variable "ssh_public_key" {
  description = "SSH public key for EC2 access (paste your ~/.ssh/squares.pub contents)"
  type        = string
}

variable "ssh_ipv4_cidr" {
  description = "IPv4 CIDR allowed to SSH into the EC2 instance"
  type        = string
  default     = "0.0.0.0/0"
}

variable "ssh_ipv6_cidr" {
  description = "IPv6 CIDR allowed to SSH into the EC2 instance"
  type        = string
  default     = "::/0"
}

# =============================================================================
# Data Sources
# =============================================================================

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

data "aws_route53_zone" "root" {
  name = "rockygray.com."
}

# SSM parameters
data "aws_ssm_parameter" "admin_token" {
  name = "/squares/admin-token"
}
