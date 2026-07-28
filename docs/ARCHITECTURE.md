# FPL Scouting System - Architecture

## Design Principles

This system is optimized for **minimal resource usage** while maintaining full functionality.

### Memory Budget: 200MB Total

| Component | Memory | Justification |
|-----------|--------|---------------|
| fpl-core | 50MB | Go binary, periodic execution |
| fpl-bot | 60MB | Go binary, persistent connection |
| Docker overhead | 20MB | Network, volumes |
| SQLite cache | 2MB | In-memory cache |
| **TOTAL** | **~132MB** | Well under 200MB target |

## Service Architecture

### Original Design (from README.md)
```
┌─────────────┐  ┌──────────────┐  ┌─────────────┐
│ fpl-fetcher │  │ fpl-analyzer │  │ fpl-telegram│
│   (Python)  │  │   (Python)   │  │    -bot     │
└─────────────┘  └──────────────┘  └─────────────┘
       │                │                  │
       └────────────────┼──────────────────┘
                        │
                  ┌─────────────┐
                  │ ClickHouse  │  ← 200MB+ RAM
                  │   (8123)    │
                  └─────────────┘
```
**Memory**: ~500MB minimum

### Optimized Design (Current)
```
┌─────────────────────────┐  ┌─────────────────────────┐
│      fpl-core           │  │       fpl-bot           │
│  (fetcher + analyzer)   │  │  (telegram + trader)    │
│      Go binary          │  │      Go binary          │
│       50MB              │  │        60MB             │
└───────────┬─────────────┘  └───────────┬─────────────┘
            │                            │
            └────────────┬───────────────┘
                         │
                   ┌─────────────┐
                   │   SQLite    │  ← 5MB RAM
                   │ (file-based)│
                   └─────────────┘
```
**Memory**: ~115MB total

## Component Details

### 1. fpl-core (Fetcher + Analyzer)

**Purpose**: Data collection and analysis

**Mode**: Cron-based periodic execution

**Responsibilities**:
- Fetch data from FPL API every 6 hours
- Store in SQLite database
- Run analysis algorithm
- Generate transfer recommendations

**Implementation**:
- Language: Go 1.21
- Dependencies:
  - `github.com/mattn/go-sqlite3` (SQLite driver)
  - `github.com/robfig/cron/v3` (scheduler)
- Binary size: ~8MB
- Runtime memory: 30-50MB

**Files**:
```
services/core/
├── main.go       # Entry point, cron scheduler
├── fetcher.go    # FPL API client
├── analyzer.go   # Recommendation engine
├── go.mod
└── Dockerfile
```

**Key Features**:
- Exponential backoff retry logic
- Batch inserts to SQLite
- Idempotent data ingestion
- Rate limiting for API calls
- Can run in two modes:
  - Daemon: Continuous cron
  - CLI: One-time execution

### 2. fpl-bot (Telegram Bot + Trader)

**Purpose**: User interface and trade execution

**Mode**: Long-running service

**Responsibilities**:
- Poll database for recommendations
- Send Telegram notifications
- Handle user confirmations
- Execute transfers via FPL API
- Log all operations

**Implementation**:
- Language: Go 1.21
- Dependencies:
  - `github.com/go-telegram-bot-api/telegram-bot-api/v5`
  - `github.com/mattn/go-sqlite3`
- Binary size: ~10MB
- Runtime memory: 40-60MB

**Files**:
```
services/bot/
├── main.go       # Entry point, Telegram bot
├── trader.go     # FPL transfer execution
├── go.mod
└── Dockerfile
```

**Key Features**:
- Long-polling Telegram updates
- Inline keyboard for confirmations
- Automatic retry on transfer failures
- Comprehensive logging
- Graceful shutdown

### 3. SQLite Database

**Purpose**: Persistent data storage

**Why SQLite?**
- ✅ File-based (no daemon process)
- ✅ <5MB memory footprint
- ✅ Zero configuration
- ✅ ACID compliant
- ✅ Perfect for single-instance deployments
- ❌ Not suitable for high concurrency (but we don't need it)

**Configuration**:
```sql
PRAGMA journal_mode=WAL;       -- Write-Ahead Logging
PRAGMA synchronous=NORMAL;      -- Balanced durability
PRAGMA cache_size=-2000;        -- 2MB cache
PRAGMA temp_store=MEMORY;       -- Temp tables in RAM
```

**Schema**:
- `players` - Current player stats
- `player_history` - Historical performance
- `fixtures` - Match schedule
- `teams` - Team information
- `recommendations` - Transfer suggestions
- `transfer_log` - Execution history
- `user_state` - Bot state
- `metadata` - System state

## Data Flow

```
1. FETCH (every 6 hours)
   ┌─────────────────────────────────┐
   │ FPL API → fpl-core → SQLite     │
   └─────────────────────────────────┘
         ↓
2. ANALYZE (30 min after fetch)
   ┌─────────────────────────────────┐
   │ SQLite → fpl-core → SQLite      │
   │ (read players → analyze → write │
   │  recommendations)                │
   └─────────────────────────────────┘
         ↓
3. NOTIFY (every hour)
   ┌─────────────────────────────────┐
   │ SQLite → fpl-bot → Telegram     │
   │ (check pending recommendations) │
   └─────────────────────────────────┘
         ↓
4. USER CONFIRMS
   ┌─────────────────────────────────┐
   │ Telegram → fpl-bot → FPL API    │
   │           → SQLite (log)        │
   └─────────────────────────────────┘
```

## Optimization Techniques

### 1. Language Choice: Go

**Why Go over Python/Node.js?**

| Metric | Python | Node.js | Go |
|--------|--------|---------|-----|
| Base memory | 50MB | 40MB | 10MB |
| Startup time | 500ms | 200ms | <50ms |
| Binary size | Interpreter | Runtime | 8-10MB |
| Dependencies | venv/pip | node_modules | Static binary |

**Result**: 5x memory reduction

### 2. Database: SQLite vs ClickHouse

| Metric | ClickHouse | SQLite |
|--------|------------|---------|
| Memory | 200MB+ | <5MB |
| Disk | 1GB+ | 50MB |
| Setup | Complex | Zero config |
| Our data size | Overkill | Perfect fit |

**Result**: 40x memory reduction

### 3. Service Consolidation

**Before**: 5 services
- fpl-fetcher
- fpl-analyzer
- fpl-telegram-bot
- fpl-trader
- clickhouse

**After**: 2 services
- fpl-core (fetcher + analyzer)
- fpl-bot (telegram + trader)

**Benefits**:
- Less Docker overhead
- Shared code/libraries
- Simpler deployment
- Lower memory usage

### 4. Docker Image Optimization

**Multi-stage build**:
```dockerfile
FROM golang:1.21-alpine AS builder  # Build stage
  → Build static binary
FROM alpine:3.19                     # Runtime stage
  → Copy binary only
```

**Result**:
- Image size: 15MB (vs 500MB+ for Python)
- No runtime dependencies
- Faster pulls and starts

### 5. Static Compilation

```bash
CGO_ENABLED=1 go build \
  -ldflags="-w -s -extldflags '-static'" \
  -tags "sqlite_omit_load_extension"
```

**Flags**:
- `-w`: Omit DWARF symbol table
- `-s`: Omit symbol table
- `-static`: Static linking
- `sqlite_omit_load_extension`: Remove unused SQLite features

**Result**: 8MB binary (vs 50MB+ for dynamic)

## Scoring Algorithm

The analyzer uses a composite scoring system:

```go
OverallScore =
  ExpectedPoints × 4.0 +  // 40%
  Value × 3.0 +            // 30%
  Form × 2.0 +             // 20%
  Availability × 10.0      // 10%
```

**Components**:
1. **ExpectedPoints**: Weighted average of PPG and recent form
2. **Value**: Points per million (cost efficiency)
3. **Form**: Current season form
4. **Availability**: Injury/suspension risk

**Recommendations**:
- Compare bottom 20% vs top 20% by position
- Filter out same-team transfers
- Sort by expected gain
- Limit to top 5 overall

## API Integration

### FPL Public API

**Endpoints**:
- `bootstrap-static/` - Players, teams, gameweeks
- `element-summary/{id}/` - Player history

**Rate Limiting**:
- 1 second delay between player fetches
- Exponential backoff on errors
- Only fetch active players (>100 minutes)

### FPL Private API

**Endpoint**:
- `my-team/{team_id}/transfer/` - Execute transfer

**Authentication**:
- Cookie-based (pl_profile)
- Requires valid session
- Expires periodically

**Payload**:
```json
{
  "transfers": [{
    "element_in": 123,
    "element_out": 456,
    "purchase_price": 85,
    "selling_price": 75
  }]
}
```

## Deployment Strategy

### Resource Allocation

```yaml
fpl-core:
  limits:
    memory: 50M
  reservations:
    memory: 30M

fpl-bot:
  limits:
    memory: 60M
  reservations:
    memory: 40M
```

**Strategy**:
- Set limits to prevent runaway processes
- Reservations ensure minimum allocation
- OOM killer terminates if limit exceeded

### Scheduling

**Core service**:
- Runs periodically via cron
- Can be stopped between runs
- Manual trigger available

**Bot service**:
- Always running (long-polling)
- Checks DB every hour
- Instant response to Telegram

## Scalability Considerations

**Current limitations**:
- Single-user (can extend to multi-user)
- Single-instance (SQLite constraint)
- UK timezone (configurable)

**Scaling options** (if needed):
1. Multi-user: Add user_id to all queries
2. Multi-instance: Switch to PostgreSQL
3. High availability: Add Redis cache
4. Analytics: Add ClickHouse for historical data

**But for 200MB constraint**: Current design is optimal.

## Security

1. **Secrets management**: Environment variables
2. **Network isolation**: Internal Docker network
3. **User permissions**: Non-root user in containers
4. **Input validation**: Telegram commands sanitized
5. **Rate limiting**: Both FPL API and Telegram

## Monitoring

**Health checks**:
```yaml
healthcheck:
  test: ["CMD", "test", "-f", "/data/fpl.db"]
  interval: 30s
```

**Metrics available**:
- Container memory usage
- Database size
- Recommendation count
- Transfer success rate

**Logs**:
- Structured logging with timestamps
- File and line numbers
- Error tracking

## Future Optimizations

If even lower memory needed:

1. **Replace cron with systemd timers** - Save 10MB
2. **Use BusyBox instead of Alpine** - Save 2MB
3. **Compile bot as CGI script** - Run only when needed
4. **Use distroless/static base** - Save 3MB

**Theoretical minimum**: ~80MB total
