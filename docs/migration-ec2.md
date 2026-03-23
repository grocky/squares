# Migration: ECS/ALB → EC2

## Cost Context

| Resource        | Before   | After   |
|-----------------|----------|---------|
| ALB             | ~$18/mo  | $0      |
| ECS Fargate     | ~$9/mo   | $0      |
| ECR             | ~$0.10   | $0      |
| t4g.micro EC2   | $0       | ~$6/mo  |
| Elastic IP      | $0       | $0 (while attached) |
| Lambda cron     | ~$0      | ~$0     |
| DynamoDB        | ~$0      | ~$0     |
| **Total**       | **~$27/mo** | **~$6/mo** |

## What Changed

**Removed:**
- `alb.tf` — Application Load Balancer and listeners
- `ecs.tf` — ECS cluster, task definition, service
- `ecr.tf` — ECR container image registry
- Two-AZ subnet requirement (ALB needs ≥2 AZs; EC2 needs only 1)
- Docker build + push + ECS redeploy flow

**Added:**
- `ec2.tf` — t4g.micro arm64 instance + Elastic IP
- `infrastructure/userdata.sh` — bootstraps Caddy + systemd service on first boot
- `make ec2-deploy` — builds arm64 binary, scps it to the instance, restarts the service

**Unchanged:**
- Lambda cron (EventBridge → score sync)
- DynamoDB table
- SSE architecture (watcher goroutine + hub — works fine without a load balancer)
- DNS (just points to EIP instead of ALB)

## VPC: Still Needed?

Yes, but only for:
- The security group (controls what ports are open to the internet)
- The subnet/IGW/route table for internet connectivity

The VPC costs $0. The only VPC-related cost driver was the ALB (which is gone).

## One-Time Setup

### 1. Generate a deploy SSH key

```bash
ssh-keygen -t ed25519 -f ~/.ssh/squares -C "squares-deploy"
```

### 2. Destroy the old ECS/ALB/ECR resources first

```bash
# Targeted destroy of the expensive resources
terraform -chdir=infrastructure destroy \
  -target=aws_lb.main \
  -target=aws_lb_listener.http \
  -target=aws_lb_listener.https \
  -target=aws_lb_target_group.server \
  -target=aws_ecs_service.server \
  -target=aws_ecs_task_definition.server \
  -target=aws_ecs_cluster.main \
  -target=aws_ecr_repository.server \
  -target=aws_iam_role.ecs_execution \
  -target=aws_iam_role.ecs_task \
  -target=aws_cloudwatch_log_group.ecs
```

### 3. Apply the new EC2 infrastructure

```bash
# Pass your new SSH public key as a variable
terraform -chdir=infrastructure apply \
  -var="ssh_public_key=$(cat ~/.ssh/squares.pub)" \
  -var="ssh_ipv4_cidr=$(curl -s -4 ifconfig.me)/32" \
  -var="ssh_ipv6_cidr=$(curl -s -6 ifconfig.me)/128"
```

> Terraform will create the EC2 instance, EIP, and update the DNS record.
> The instance will bootstrap itself (install Caddy, set up systemd) on first boot — takes ~2 minutes.

### 4. Deploy the app binary

```bash
make ec2-deploy
```

This builds a `linux/arm64` binary and scps it to `/opt/squares/squares-server`, then restarts the systemd service.

### 5. Verify

```bash
# Check service is running
make ec2-logs

# Hit the health endpoint
curl https://squares.rockygray.com/health

# Confirm SSE connects
curl -N https://squares.rockygray.com/pools/main/events
```

## Ongoing Deploys

```bash
# Deploy new code (rebuild + scp + restart)
make ec2-deploy

# Deploy with explicit host (if terraform output isn't available)
make ec2-deploy EC2_HOST=<ip>

# Tail logs
make ec2-logs

# SSH in
make ec2-ssh
```

## SSE Notes

SSE works better on EC2+Caddy than on ALB because:
- ALB had a configurable idle timeout (we set it to 3600s as a workaround)
- Caddy uses `flush_interval -1` which flushes SSE frames immediately with no timeout
- Single instance = no sticky session concern; all clients connect to the same SSE hub
