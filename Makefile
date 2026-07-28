.PHONY: help build up down logs restart clean status fetch analyze shell-core shell-bot

REMOTE_HOST ?= 103.200.22.207
REMOTE_USER ?= root
REMOTE_PATH ?= /root/fpl-scouting

help:
	@echo "FPL Scouting System - Available Commands:"
	@echo ""
	@echo "Local:"
	@echo "  make build       - Build all Docker images"
	@echo "  make up          - Start all services"
	@echo "  make down        - Stop all services"
	@echo "  make restart     - Restart all services"
	@echo "  make logs        - View logs from all services"
	@echo "  make logs-core   - View logs from core service"
	@echo "  make logs-bot    - View logs from bot service"
	@echo "  make status      - Show service status"
	@echo "  make fetch       - Manually trigger data fetch"
	@echo "  make analyze     - Manually trigger analysis"
	@echo "  make clean       - Remove all containers and volumes"
	@echo "  make shell-core  - Open shell in core container"
	@echo "  make shell-bot   - Open shell in bot container"
	@echo ""
	@echo "Remote (server: $(REMOTE_HOST), path: $(REMOTE_PATH)):"
	@echo "  make remote-build     - Rebuild images on server"
	@echo "  make remote-up        - Start services on server"
	@echo "  make remote-down      - Stop services on server"
	@echo "  make remote-restart   - Restart services on server"
	@echo "  make remote-logs      - Tail logs on server"
	@echo "  make remote-status    - Show server status"
	@echo "  make remote-fetch     - Trigger data fetch on server"
	@echo "  make remote-analyze   - Trigger analysis on server"
	@echo "  make remote-shell-core- SSH into core container"
	@echo "  make remote-shell-bot - SSH into bot container"
	@echo ""
	@echo "  make db-recs     - View recommendations (auto-remote if REMOTE_HOST set)"
	@echo "  make db-players  - View players"
	@echo "  make db-status   - View metadata/status"
	@echo "  make db-history  - View player history"
	@echo "  make db-copy     - Copy SQLite DB locally"
	@echo "  make db-sql      - Direct SQL query tool"
	@echo ""

build:
	docker compose build

up:
	docker compose up -d
	@echo "Services started. Use 'make logs' to view logs."

down:
	docker compose down

restart:
	docker compose restart

logs:
	docker compose logs -f

logs-core:
	docker compose logs -f fpl-core

logs-bot:
	docker compose logs -f fpl-bot

status:
	docker compose ps
	@echo ""
	@echo "Memory usage:"
	@docker stats --no-stream --format "table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}"

fetch:
	docker compose exec fpl-core /app/fpl-core -once -fetch

analyze:
	docker compose exec fpl-core /app/fpl-core -once -analyze

clean:
	docker compose down -v
	@echo "All containers and volumes removed."

shell-core:
	docker compose exec fpl-core sh

shell-bot:
	docker compose exec fpl-bot sh

# Development helpers
dev-build:
	docker compose build --no-cache

dev-up:
	docker compose up

install:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "Created .env file. Please edit it with your configuration."; \
	else \
		echo ".env file already exists."; \
	fi

# Remote management (SSH to server)
remote-build:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose build"

remote-up:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose up -d"
	@echo "Services started on $(REMOTE_HOST)."

remote-down:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose down"

remote-restart:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose restart"

remote-logs:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose logs -f"

remote-status:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose ps && echo '' && docker stats --no-stream --format 'table {{.Name}}\t{{.MemUsage}}\t{{.MemPerc}}'"

remote-fetch:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core /app/fpl-core -once -fetch"

remote-analyze:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core /app/fpl-core -once -analyze"

remote-shell-core:
	ssh -t $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec fpl-core sh"

remote-shell-bot:
	ssh -t $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec fpl-bot sh"

# Database queries (local by default, remote if REMOTE_HOST is set)
ifeq ($(REMOTE_HOST),)
db-recs:
	docker compose exec fpl-core sh /app/db-query.sh recommendations

db-players:
	docker compose exec fpl-core sh /app/db-query.sh players

db-status:
	docker compose exec fpl-core sh /app/db-query.sh status

db-user:
	docker compose exec fpl-core sh /app/db-query.sh user

db-history:
	docker compose exec fpl-core sh /app/db-query.sh history

db-sql:
	docker compose exec fpl-core sh /app/db-query.sh sql

db-copy:
	docker compose cp fpl-core:/data/fpl.db ./fpl-local.db
	@echo "Database copied to ./fpl-local.db"
else
db-recs:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh recommendations"

db-players:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh players"

db-status:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh status"

db-user:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh user"

db-history:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh history"

db-sql:
	ssh -t $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose exec -T fpl-core sh /app/db-query.sh sql"

db-copy:
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "cd $(REMOTE_PATH) && docker compose cp fpl-core:/data/fpl.db /tmp/fpl-remote.db"
	scp $(REMOTE_USER)@$(REMOTE_HOST):/tmp/fpl-remote.db ./fpl-local.db
	ssh $(REMOTE_USER)@$(REMOTE_HOST) "rm /tmp/fpl-remote.db"
	@echo "Database copied from $(REMOTE_HOST) to ./fpl-local.db"
endif
