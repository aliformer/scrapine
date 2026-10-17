#!/bin/bash
set -e

# Run inside Debian VM after first boot

# Install Docker
apt update && apt install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list
apt update && apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# Create directories
mkdir -p /opt/gmaps/{output,queries}

echo "Docker installed. Copy docker-compose.yml and queries to /opt/gmaps/"
echo "Then: cd /opt/gmaps && docker compose up -d"
