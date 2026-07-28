# FPL Scouting System - Deployment Guide

## Overview

This is an optimized FPL (Fantasy Premier League) scouting system designed to run on a **2 CPU, 2GB RAM, 40GB SSD** server with a **200MB memory footprint**.

### Architecture

**Optimized for Low Memory:**
- **fpl-core** (50MB): Fetches FPL data and generates transfer recommendations
- **fpl-bot** (60MB): Telegram bot + auto-trader
- **SQLite**: Lightweight file-based database (shared volume)
- **Total RAM**: ~110MB peak, ~60MB idle

### Key Optimizations
- All services written in **Go** (not Python/Node.js)
- **Alpine Linux** base images (5MB vs 100MB+)
- **SQLite** instead of ClickHouse (5MB vs 200MB)
- Merged services to reduce Docker overhead
- Static compilation with aggressive size optimization

## Prerequisites

1. **Docker & Docker Compose** installed on your server
2. **Telegram Bot Token** - Get from [@BotFather](https://t.me/botfather)
3. **FPL Account** credentials (optional, for auto-trading)

## Quick Start

### 1. Clone and Setup

```bash
git clone <your-repo-url>
cd fpl-scouting

# Create .env file from template
make install

# Edit .env with your configuration
nano .env
```

### 2. Configure Environment Variables

Edit `.env` file:

```bash
# Required: Telegram Bot Token
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrsTUVwxyz

# Optional: FPL credentials for auto-trading
FPL_SESSION_COOKIE=your_session_cookie_here
FPL_TEAM_ID=your_team_id

# Scheduling (default: fetch every 6 hours)
FETCH_SCHEDULE=0 */6 * * *
ANALYZE_SCHEDULE=30 */6 * * *
```

**Getting FPL Credentials:**
1. Login to https://fantasy.premierleague.com
2. Open browser DevTools (F12) → Application → Cookies
3. Copy `pl_profile` cookie value
4. Your team ID is in the URL: `https://fantasy.premierleague.com/entry/{TEAM_ID}/`

### 3. Build and Deploy

```bash
# Build images
make build

# Start services
make up

# Check status
make status
```

### 4. Verify Deployment

```bash
# View logs
make logs

# Check memory usage
make status

# Manual test - fetch data
make fetch

# Manual test - analyze
make analyze
```

Expected output:
```
NAME       MEM USAGE   MEM %
fpl-core   35MB/50MB   1.7%
fpl-bot    42MB/60MB   2.1%
```

### 5. Start Using Telegram Bot

1. Open Telegram and search for your bot
2. Send `/start` to initialize
3. Send `/status` to check system status
4. Wait for recommendations (or trigger with `/recommendations`)

## Configuration

### Memory Limits

In `docker-compose.yaml`:
```yaml
deploy:
  resources:
    limits:
      memory: 50M  # Hard limit
    reservations:
      memory: 30M  # Soft limit
```

**Adjust if needed** based on your server capacity.

### Scheduling

Cron format in `.env`:
- `0 */6 * * *` = Every 6 hours
- `0 0 * * *` = Daily at midnight
- `*/30 * * * *` = Every 30 minutes

**Recommended:**
- Fetch: Every 6 hours (API rate limits)
- Analyze: 30 min after fetch
- Bot check: Every 60 minutes

### Manual Operations

```bash
# Fetch latest FPL data
make fetch

# Run analysis
make analyze

# View core service logs
make logs-core

# View bot logs
make logs-bot

# Restart services
make restart
```

## Monitoring

### Check Service Health

```bash
# Container status
docker-compose ps

# Memory usage
docker stats --no-stream

# Detailed logs
make logs
```

### Database Inspection

```bash
# Access core container
make shell-core

# Inside container, query SQLite
sqlite3 /data/fpl.db "SELECT COUNT(*) FROM players;"
sqlite3 /data/fpl.db "SELECT * FROM recommendations WHERE status='pending';"
```

## Troubleshooting

### Bot Not Responding

1. Check bot token: `docker-compose logs fpl-bot | grep -i auth`
2. Verify network: `docker network ls`
3. Check chat ID registered: Send `/start` to bot

### No Recommendations

1. Check if data fetched: `make logs-core`
2. Manually trigger: `make fetch && make analyze`
3. Check database: `make shell-core` then `sqlite3 /data/fpl.db "SELECT * FROM recommendations;"`

### High Memory Usage

1. Check current usage: `docker stats`
2. Adjust limits in `docker-compose.yaml`
3. Consider stopping core service between runs:
   ```bash
   docker-compose stop fpl-core
   # Run manually when needed
   make fetch
   make analyze
   ```

### Transfer Execution Failing

1. Verify FPL credentials in `.env`
2. Check session cookie hasn't expired (login again)
3. Check logs: `make logs-bot | grep -i transfer`

## Backup & Recovery

### Backup Database

```bash
# Backup SQLite database
docker-compose exec fpl-core cp /data/fpl.db /data/fpl.db.backup

# Copy to host
docker cp fpl-core:/data/fpl.db ./backups/fpl.db.$(date +%Y%m%d)
```

### Restore Database

```bash
# Copy backup to container
docker cp ./backups/fpl.db.20241121 fpl-core:/data/fpl.db

# Restart services
make restart
```

## Updating

```bash
# Pull latest code
git pull

# Rebuild images
make build

# Restart with new images
make down
make up
```

## Resource Usage

**Expected resource usage:**

| Metric | Idle | Peak |
|--------|------|------|
| Memory | 60MB | 110MB |
| CPU | <5% | 20-30% (during fetch/analyze) |
| Disk | ~50MB | ~500MB (with data) |
| Network | Minimal | <10MB/hour |

**Server Requirements:**
- ✅ 2 CPU cores
- ✅ 2GB RAM (with 1.8GB free for other apps)
- ✅ 40GB SSD (plenty of space)
- ✅ Internet connection

## Security Notes

1. **Never commit `.env`** - Contains sensitive tokens
2. **Rotate FPL session** - Cookie expires, update regularly
3. **Firewall** - Only expose necessary ports
4. **Updates** - Regularly rebuild images for security patches

## Advanced Configuration

### Run Core Service on Demand Only

To save even more memory, run core service manually:

```yaml
# In docker-compose.yaml, comment out fpl-core or add:
profiles: ["manual"]
```

Then trigger manually:
```bash
docker-compose run --rm fpl-core /app/fpl-core -once
```

### Multiple Users

Edit `sql/schema.sql` to add multiple user configurations in `user_state` table.

### Custom Scoring Algorithm

Edit `services/core/analyzer.go` → `calculateOverallScore()` function.

## Support

- Issues: Create GitHub issue
- Logs: Always check `make logs` first
- Status: Use `make status` for quick health check

## Performance Tips

1. **Reduce fetch frequency** if API rate-limited
2. **Only fetch active players** (>100 minutes played)
3. **Limit recommendations** to top 5 (already implemented)
4. **Use SQLite WAL mode** (already enabled)
5. **Stop core between runs** if memory critical

## Uninstall

```bash
# Stop and remove everything
make clean

# Remove images
docker-compose down --rmi all

# Remove project
cd .. && rm -rf fpl-scouting
```
