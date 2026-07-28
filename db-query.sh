#!/bin/bash
# FPL Database Query Helper

DB_PATH="/data/fpl.db"

echo "=== FPL Scouting Database ==="
echo ""

# Install sqlite if not present
apk add --no-cache sqlite >/dev/null 2>&1

case "$1" in
  recommendations|recs)
    echo "📊 Latest Recommendations:"
    sqlite3 $DB_PATH <<EOF
.mode column
.headers on
SELECT 
  id,
  sell_player_name AS 'Sell',
  buy_player_name AS 'Buy',
  ROUND(expected_points_gain, 1) AS 'Gain',
  ROUND(price_diff, 1) AS 'Price Diff',
  status
FROM recommendations 
ORDER BY timestamp DESC 
LIMIT 10;
EOF
    ;;
    
  players|top)
    echo "⭐ Top Players by Points:"
    sqlite3 $DB_PATH <<EOF
.mode column
.headers on
SELECT 
  id,
  web_name AS 'Name',
  element_type AS 'Pos',
  total_points AS 'Points',
  ROUND(CAST(now_cost AS REAL)/10, 1) AS 'Price',
  ROUND(CAST(form AS REAL), 1) AS 'Form'
FROM players 
ORDER BY total_points DESC 
LIMIT 15;
EOF
    ;;
    
  status)
    echo "📈 System Status:"
    sqlite3 $DB_PATH <<EOF
.mode column
.headers on
SELECT key, value FROM metadata;
SELECT 'Players' AS metric, COUNT(*) AS count FROM players
UNION ALL
SELECT 'Recommendations', COUNT(*) FROM recommendations
UNION ALL
SELECT 'Pending Recs', COUNT(*) FROM recommendations WHERE status='pending';
EOF
    ;;
    
  user)
    echo "👤 User State:"
    sqlite3 $DB_PATH <<EOF
.mode column
.headers on
SELECT * FROM user_state;
EOF
    ;;
    
  history)
    echo "📜 Recent Player History:"
    sqlite3 $DB_PATH <<EOF
.mode column
.headers on
SELECT 
  player_id,
  event AS 'GW',
  total_points AS 'Pts',
  minutes AS 'Min',
  goals_scored AS 'G',
  assists AS 'A'
FROM player_history 
ORDER BY event DESC, total_points DESC 
LIMIT 20;
EOF
    ;;
    
  sql)
    echo "🔍 SQL Shell (type .quit to exit):"
    sqlite3 $DB_PATH
    ;;
    
  *)
    echo "Usage: $0 {recommendations|players|status|user|history|sql}"
    echo ""
    echo "Commands:"
    echo "  recommendations  - Show latest transfer recommendations"
    echo "  players          - Show top players by points"
    echo "  status           - Show system status"
    echo "  user             - Show user state"
    echo "  history          - Show recent player history"
    echo "  sql              - Open SQLite shell"
    echo ""
    echo "Examples:"
    echo "  docker compose exec fpl-core sh /app/db-query.sh recommendations"
    echo "  docker compose exec fpl-core sh /app/db-query.sh players"
    echo "  docker compose exec fpl-core sh /app/db-query.sh sql"
    ;;
esac
