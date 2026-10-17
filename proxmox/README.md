# Google Maps Scraper on Proxmox — Deployment Guide

## Files Overview

| File | Purpose |
|------|---------|
| `create-vm.sh` | Bash script to create VM on Proxmox host |
| `setup-vm.sh` | Run inside VM after first boot to install Docker |
| `docker-compose.yml` | Distributed mode: scraper + PostgreSQL backend |
| `docker-compose.web.yml` | Web UI mode: interactive browser interface |
| `queries/queries.txt` | Sample input file (one query per line) |
| `firewall-200.fw` | Proxmox firewall rules for port 58080 |

## Deployment Steps

### 1. Proxmox Host: Create VM

Option A — Script:
```bash
chmod +x create-vm.sh
./create-vm.sh
```

Option B — GUI:
- Datacenter → Create VM
- ID: 200, Name: gmaps-scraper
- Memory: 4GB, vCPU: 4
- Storage: 32GB (local-lvm)
- ISO: Debian 12 genericcloud
- Network: vmbr0 (default)

### 2. Start VM & Get IP

```bash
qm start 200
qm guest cmd 200 network get-interfaces
# or: ssh into VM once booted, check 'ip addr'
```

### 3. SSH into VM

```bash
ssh root@<VM-IP>
```

### 4. Inside VM: Install Docker

```bash
chmod +x setup-vm.sh
./setup-vm.sh
```

Or manually:
```bash
apt update && apt install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list
apt update && apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
```

### 5. Copy Config Files to VM

```bash
# From your workstation:
scp docker-compose.yml root@<VM-IP>:/opt/gmaps/
scp -r queries/ root@<VM-IP>:/opt/gmaps/
```

### 6. Start Services

**Distributed mode (recommended for production):**
```bash
cd /opt/gmaps
docker compose up -d
docker compose logs -f scraper
```

**Web UI mode (for interactive use):**
```bash
docker compose -f docker-compose.web.yml up -d
# Access: http://<VM-IP>:58080
```

### 7. Check Results

Distributed mode:
```bash
docker compose exec scraper ls /out/
docker compose exec scraper head /out/*.csv
```

Web UI:
```bash
curl http://<VM-IP>:58080/status
```

## Proxmox Firewall (Optional)

If Proxmox firewall enabled on datacenter/node level:

```bash
scp firewall-200.fw root@<PROXMOX-HOST>:/etc/pve/firewall/200.fw
pvefw enable
```

Or manually edit `/etc/pve/firewall/200.fw` on Proxmox host.

## Resource Tuning

**For `-c 8` (8 concurrent browsers):**
- Edit `/etc/pve/nodes/<node>/qemu-server/200.conf` on Proxmox
- Increase: `memory: 8192`, `cores: 8`
- Reboot VM

Or via GUI: VM → Hardware → Memory/Processor → Resize

## Troubleshooting

### Docker image pull fails
```bash
docker pull gosom/google-maps-scraper:latest
```

### Chromium sandbox issues (shouldn't happen in VM)
Add to docker-compose: `--security-opt seccomp=unconfined` (not recommended; indicates environment problem)

### PostgreSQL connection timeout
```bash
docker compose logs db
docker compose exec db psql -U gmaps -d gmaps -c "SELECT 1"
```

### Out of disk space
```bash
docker system prune -a
# Or increase VM disk in Proxmox GUI
```

## Verify Installation

Inside VM:
```bash
docker ps  # Should show db + scraper/web containers
docker compose logs -f scraper
```

Expected output in scraper logs:
```
Starting scrape for query: restaurants in Berlin
Fetched results: ...
```

## Next Steps

- Edit `queries/queries.txt` with your search terms
- Monitor: `docker compose logs -f scraper`
- Export results: `docker compose exec scraper cat /out/results.csv`
- Scale: increase `-c` flag or replicate VM + point to same PostgreSQL
