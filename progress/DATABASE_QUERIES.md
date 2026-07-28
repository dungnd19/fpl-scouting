# Database Query Commands

## Quick Commands

Add these to your Makefile for easy access:

```makefile
# Database queries
db-recs:
	docker compose exec fpl-core sh /app/db-query.sh recommendations

db-players:
	docker compose exec fpl-core sh /app/db-query.sh players

db-status:
	docker compose exec fpl-core sh /app/db-query.sh status

db-user:
	docker compose exec fpl-core sh /app/db-query.sh user

db-sql:
	docker compose exec fpl-core sh /app/db-query.sh sql
```

## Usage Examples

```bash
# View latest recommendations
make db-recs

# View top players
make db-players

# Check system status
make db-status

# Open SQL shell for custom queries
make db-sql
```

## Manual Queries

```bash
# Access SQLite directly
docker compose exec fpl-core sh -c "apk add sqlite && sqlite3 /data/fpl.db"

# Then run queries like:
SELECT * FROM recommendations WHERE status='pending';
SELECT web_name, total_points FROM players ORDER BY total_points DESC LIMIT 10;
```

## Copy Database to Local

```bash
# Copy the database file to your local machine
docker compose cp fpl-core:/data/fpl.db ./fpl-local.db

# Then open with any SQLite browser
sqlite3 fpl-local.db
# or use DB Browser for SQLite (GUI)
```
