#!/bin/bash
set -euo pipefail

# =============================================================================
# User data — runs once at first boot on the squares EC2 instance
# Installs Caddy + sets up the squares server as a systemd service.
# The binary itself is deployed via `make ec2-deploy` (scp + restart).
# =============================================================================

DOMAIN="${domain}"
ADMIN_TOKEN_PATH="${admin_token_path}"
POOL_ID="${pool_id}"
DYNAMODB_TABLE="${dynamodb_table}"
AWS_REGION="${aws_region}"
APP_USER="squares"
APP_DIR="/opt/squares"

# ---------------------------------------------------------------------------
# Create app user + directory early (before anything else)
# so `make ec2-deploy` can succeed even if the rest of user data is still running
# ---------------------------------------------------------------------------
useradd --system --no-create-home --shell /sbin/nologin "$APP_USER" || true
mkdir -p "$APP_DIR"
chown "$APP_USER:$APP_USER" "$APP_DIR"
chown "$APP_USER:$APP_USER" /etc/squares

# ---------------------------------------------------------------------------
# System deps
# ---------------------------------------------------------------------------
dnf update -y

# ---------------------------------------------------------------------------
# Install Caddy — download binary directly from GitHub (avoids curl-minimal
# conflict introduced by the Caddy RPM repo on AL2023)
# ---------------------------------------------------------------------------
CADDY_VERSION="${caddy_version}"
curl -fsSL "https://github.com/caddyserver/caddy/releases/download/v$${CADDY_VERSION}/caddy_$${CADDY_VERSION}_linux_arm64.tar.gz" \
  | tar -xz -C /usr/local/bin caddy
chmod +x /usr/local/bin/caddy

# Create caddy system user and required dirs (mirrors what the RPM would do)
useradd --system --no-create-home --shell /sbin/nologin caddy || true
mkdir -p /etc/caddy /var/lib/caddy /var/log/caddy
chown caddy:caddy /var/lib/caddy /var/log/caddy

# systemd service for Caddy
cat > /etc/systemd/system/caddy.service <<'CADDYSVC'
[Unit]
Description=Caddy
Documentation=https://caddyserver.com/docs/
After=network.target network-online.target
Requires=network-online.target

[Service]
Type=notify
User=caddy
Group=caddy
Environment=HOME=/var/lib/caddy
Environment=XDG_DATA_HOME=/var/lib/caddy
Environment=XDG_CONFIG_HOME=/var/lib/caddy/.config
ExecStart=/usr/local/bin/caddy run --environ --config /etc/caddy/Caddyfile
ExecReload=/usr/local/bin/caddy reload --config /etc/caddy/Caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=1048576
PrivateTmp=true
ProtectSystem=full
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
CADDYSVC

# ---------------------------------------------------------------------------
# Caddy config — reverse proxy with auto-TLS
# SSE works natively: Caddy flushes chunked responses immediately.
# ---------------------------------------------------------------------------
cat > /etc/caddy/Caddyfile <<EOF
$DOMAIN {
    reverse_proxy localhost:8080 {
        # Flush immediately for SSE / chunked streaming
        flush_interval -1
    }
}
EOF

# ---------------------------------------------------------------------------
# systemd service for the squares server
# Reads ADMIN_TOKEN from SSM Parameter Store at startup via aws cli.
# ---------------------------------------------------------------------------
cat > /etc/systemd/system/squares.service <<EOF
[Unit]
Description=Squares server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$APP_USER
WorkingDirectory=$APP_DIR
EnvironmentFile=-/etc/squares/env
ExecStartPre=/bin/bash -c 'echo "ADMIN_TOKEN=\$(aws ssm get-parameter --name $ADMIN_TOKEN_PATH --with-decryption --query Parameter.Value --output text --region $AWS_REGION)" > /etc/squares/env'
ExecStart=$APP_DIR/squares-server
Restart=always
RestartSec=5s

# Env vars baked in (non-secret)
Environment=PORT=8080
Environment=POOL_ID=$POOL_ID
Environment=DYNAMODB_TABLE=$DYNAMODB_TABLE
Environment=AWS_REGION=$AWS_REGION

[Install]
WantedBy=multi-user.target
EOF

mkdir -p /etc/squares
chown squares:squares /etc/squares
chmod 700 /etc/squares

# ---------------------------------------------------------------------------
# Enable services (they'll start on next boot / after binary is deployed)
# ---------------------------------------------------------------------------
systemctl daemon-reload
systemctl enable caddy
systemctl enable squares

# Start Caddy now so it can obtain the TLS cert while DNS propagates
systemctl start caddy

echo "Bootstrap complete. Deploy the binary with: make ec2-deploy"
