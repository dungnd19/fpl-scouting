.PHONY: help build up down logs restart clean status fetch deploy shell-core shell-bot

help:
	@echo "FPL Scouting System - Available Commands:"
	@echo ""
	@echo "  make build       - Build all Docker images"
	@echo "  make up          - Start all services"
	@echo "  make down        - Stop all services"
	@echo "  make restart     - Restart all services"
	@echo "  make logs        - View logs from all services"
	@echo "  make logs-core   - View logs from core service"
	@echo "  make logs-bot    - View logs from bot service"
	@echo "  make status      - Show service status"
	@echo "  make fetch       - Manually trigger data fetch"
	@echo "  make deploy      - git pull, rebuild, and recreate containers (DB refetches on startup)"
	@echo "  make clean       - Remove all containers and volumes"
	@echo "  make shell-core  - Open shell in core container"
	@echo "  make shell-bot   - Open shell in bot container"
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

deploy:
	git pull
	docker compose up -d --build
	@echo "Deployed. fpl-core refetches on startup, so the DB will be current within seconds."

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

# Database queries
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
