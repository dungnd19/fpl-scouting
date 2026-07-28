# FPL Scouting System Specification

This document defines all services in the **docker-compose** stack and provides precise, coding-agent-ready specifications.

---

## Overall Architecture

The system contains 5 services:

1. **fpl-fetcher** – periodically fetches raw FPL API data and stores to ClickHouse.
2. **fpl-analyzer** – computes metrics, predictions, and recommended transfers.
3. **fpl-telegram-bot** – sends recommendations to Telegram and accepts confirmations.
4. **fpl-trader** – executes transfers via private API when receiving webhook from Telegram bot.
5. **clickhouse** – stores persistent data for all services.

All services communicate over internal Docker network.

---

## 1. Service: fpl-fetcher

**Purpose:** Fetch official FPL API data and persist into ClickHouse tables.

### Responsibilities

* Pull periodic data from:

  * `https://fantasy.premierleague.com/api/bootstrap-static/`
  * `https://fantasy.premierleague.com/api/element-summary/{id}/`
  * (Optional) user team endpoint using session cookie
* Normalize and write into ClickHouse tables:

  * `players`
  * `player_history`
  * `fixtures`
* Ensure idempotent ingestion (dedupe by `event` and `player_id`).

### Inputs

* FPL session cookie (optional) via environment variable.
* Schedule (interval) via environment variable.

### Outputs

* Writes rows to ClickHouse.

### Code Agent Requirements

* Use Python or Go.
* Must retry failed API calls with exponential backoff.
* Must batch insert into ClickHouse.
* Must use persistent storage.

---

## 2. Service: fpl-analyzer

**Purpose:** Read raw tables and compute predictions + recommended transfers.

### Responsibilities

* Compute player metrics:

  * expected points
  * value (points/price)
  * form/streak metric
  * fixture difficulty weighting
  * risk score (rotation, injury)
* Compute team-level metrics:

  * squad balance
  * captain prediction
  * transfer recommendations (`sell`, `buy`, `reason`, `score`)
* Produce output JSON into ClickHouse table `recommendations`.

### Inputs

* ClickHouse tables: `players`, `player_history`, `fixtures`.

### Outputs

* ClickHouse table `recommendations` with structure:

  ```
  timestamp DateTime
  user_id String
  transfers Array(String)
  scores Array(Float64)
  meta String
  ```

### Code Agent Requirements

* Use Python, Go or Java.
* Implement a pluggable scoring algorithm.
* Should support CLI/manual run and cron-like periodic run.

---

## 3. Service: fpl-telegram-bot

**Purpose:** Notify user of recommended transfers; receive confirmation.

### Responsibilities

* Poll ClickHouse for the latest recommendations.
* Send message to Telegram via Bot API.
* Include inline keyboard:

  * **Confirm Transfer** → send webhook to `fpl-trader`.
* Maintain per-user state.

### Inputs

* Telegram Bot Token.
* ClickHouse database.

### Outputs

* Webhook call to `fpl-trader`:

  ```json
  {
    "user_id": "123",
    "transfers": ["SELL_xx", "BUY_yy"],
    "timestamp": "..."
  }
  ```

### Code Agent Requirements

* Use Python/aiogram or Node.js.
* Must handle Telegram webhook or polling mode.

---

## 4. Service: fpl-trader

**Purpose:** Execute trades using private API with user session cookie.

### Responsibilities

* Receive POST webhook from telegram bot.
* Validate command.
* Call private FPL API:

  * `https://fantasy.premierleague.com/api/my-team/{team_id}/transfer/`
* Rate-limit requests.
* Log the result into ClickHouse table `transfer_log`.

### Inputs

* Session cookie.
* User team ID.

### Outputs

* `transfer_log` table with fields:

  ```
  timestamp DateTime
  user_id String
  request String
  response String
  status String
  ```

### Code Agent Requirements

* Use Python or Go.
* Ensure safe execution (no duplicate transfers).

---

## 5. Database: ClickHouse

**Purpose:** Persistent analytics store.

### Requirements

* Use mounted volume for persistence.
* Expose port 8123 internally.
* Tables:

  * `players`
  * `player_history`
  * `fixtures`
  * `recommendations`
  * `transfer_log`

---

## docker-compose.yaml Specification

This section specifies how the coding agent must generate `docker-compose.yaml`.

### Services

* `clickhouse`
* `fpl-fetcher`
* `fpl-analyzer`
* `fpl-telegram-bot`
* `fpl-trader`

### Requirements

* All services must share a single network `fpl-net`.
* ClickHouse must mount persistent volumes.
* Each service must use environment variables via `.env`.
* Each service must auto-restart.

Example directory layout the coding agent should create:

```
fpl/
  docker-compose.yaml
  .env
  services/
    fetcher/
    analyzer/
    telegram_bot/
    trader/
  sql/
    create_tables.sql
  docs/
    fpl-scouting.md
```

---

## Task Summary for Coding Agent

1. Generate full `docker-compose.yaml`.
2. Scaffold five service folders.
3. Implement ClickHouse schema creation script.
4. Implement code templates for each service.
5. Enable persistent volumes.
6. Implement a Makefile:

   * `make up`
   * `make down`
   * `make logs`
7. Implement CI/CD building each service container.

---

This specification is complete and suitable for automated implementation.

