# Proxmox Setup for Google Maps Scraper

## Context

Deploy google-maps-scraper on Proxmox. Scraper uses Playwright/Chromium headless browser; needs adequate CPU/RAM. Docker image available.

## Approach

### 1. Create Debian VM (not LXC)

LXC + Chromium = seccomp/namespace pain. VM simpler.

```bash
# Proxmox shell
qm create 200 \
  --name gmaps-scraper \
  --memory 4096 \
  --cores 4 \
  --cpu cputype=host \
  --net0 virtio,bridge=vmbr0 \
  --scsihw virtio-scsi-pci \
  --scsi0 local-lvm:32 \
  --ide2 local:iso/debian-12-genericcloud-amd64.qcow2,media=cdrom \
  --boot order=scsi0 \
  --ostype l26 \
  --agent enabled=1
```

Or via GUI: Datacenter → Create VM → Debian 12, 4 cores, 4GB RAM, 32GB disk.

**Resource sizing:**
| Concurrency | RAM | vCPU |
|-------------|-----|------|
| `-c 2` | 2GB | 2 |
| `-c 4` | 4GB | 4 |
| `-c 8` | 8GB | 8 |

### 2. Install Docker inside VM

```bash
# SSH into VM
apt update && apt install -y ca-certificates curl gnupg
install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/debian/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list
apt update && apt install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
```

### 3. Create working directories

```bash
mkdir -p /opt/gmaps/{output,queries}
echo "restaurants in Berlin" > /opt/gmaps/queries/queries.txt
```

### 4. Run scraper (CLI mode)

```bash
docker run --rm \
  -v gmaps-playwright-cache:/opt \
  -v /opt/gmaps/queries/queries.txt:/queries.txt:ro \
  -v /opt/gmaps/output:/out \
  gosom/google-maps-scraper:latest \
  -input /queries.txt \
  -results /out/results.csv \
  -c 2 -depth 1 \
  -exit-on-inactivity 3m
```

### 5. Run with PostgreSQL (distributed mode)

Create `docker-compose.yml` at `/opt/gmaps/`:

```yaml
services:
  db:
    image: postgres:15-alpine
    environment:
      POSTGRES_USER: gmaps
      POSTGRES_PASSWORD: changeme
      POSTGRES_DB: gmaps
    volumes:
      - pgdata:/var/lib/postgresql/data
    restart: unless-stopped

  scraper:
    image: gosom/google-maps-scraper:latest
    depends_on:
      - db
    volumes:
      - playwright-cache:/opt
      - ./queries:/queries:ro
      - ./output:/out
    command: >
      -dsn postgres://gmaps:changeme@db:5432/gmaps?sslmode=disable
      -c 2 -depth 1
      -exit-on-inactivity 5m
    restart: unless-stopped

volumes:
  pgdata:
  playwright-cache:
```

Run:
```bash
cd /opt/gmaps
docker compose up -d
```

### 6. Optional: Web UI mode

```bash
docker run -d --name gmaps-web \
  -p 58080:58080 \
  -v gmaps-playwright-cache:/opt \
  -v /opt/gmaps/webdata:/webdata \
  gosom/google-maps-scraper:latest \
  -web -addr :58080 -data-folder /webdata
```

Access at `http://<VM-IP>:58080`

### 7. Proxmox firewall (if enabled)

Allow port 58080 for web UI:
```bash
# /etc/pve/firewall/200.fw
[RULES]
IN ACCEPT -p tcp -dport 58080
```

## Critical files & anchors

| File | Purpose |
|------|---------|
| `Dockerfile` | Image build; shows Playwright deps |
| `docker-compose.dev.yaml` | PostgreSQL reference config |
| `README.md:490-530` | CLI options reference |

## Verification

1. VM boots, Docker runs: `docker info`
2. Basic scrape: run CLI command above, check `/opt/gmaps/output/results.csv` has data
3. Web UI: curl `http://localhost:58080` returns HTML

## Assumptions

- Using Proxmox default `vmbr0` bridge for networking
- Local-lvm storage exists; adjust `--scsi0` if different
- Debian 12 cloud image; substitute your ISO path
