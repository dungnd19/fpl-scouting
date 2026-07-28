#!/bin/bash

# FPL Scouting System - Setup Script
# This script helps you set up the system for the first time

set -e

echo "=================================="
echo "FPL Scouting System - Setup"
echo "=================================="
echo ""

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    echo "❌ Docker is not installed. Please install Docker first."
    echo "   Visit: https://docs.docker.com/get-docker/"
    exit 1
fi

echo "✅ Docker found: $(docker --version)"

# Check if Docker Compose v2 plugin is available
if ! docker compose version &> /dev/null; then
    echo "❌ Docker Compose plugin not found. Please install it."
    echo "   Visit: https://docs.docker.com/compose/install/"
    exit 1
fi

echo "✅ Docker Compose found: $(docker compose version)"
echo ""

# Create .env if it doesn't exist
if [ ! -f .env ]; then
    echo "📝 Creating .env file from template..."
    cp .env.example .env
    echo "✅ .env file created"
else
    echo "ℹ️  .env file already exists"
fi

echo ""
echo "=================================="
echo "Configuration Required"
echo "=================================="
echo ""
echo "Please configure the following in your .env file:"
echo ""
echo "1. TELEGRAM_BOT_TOKEN (REQUIRED)"
echo "   - Get from @BotFather on Telegram"
echo "   - Visit: https://t.me/botfather"
echo ""
echo "2. FPL_SESSION_COOKIE (Optional - for auto-trading)"
echo "   - Login to fantasy.premierleague.com"
echo "   - Open DevTools (F12) → Application → Cookies"
echo "   - Copy 'pl_profile' cookie value"
echo ""
echo "3. FPL_TEAM_ID (Optional - for auto-trading)"
echo "   - Your team ID from the URL"
echo "   - Example: https://fantasy.premierleague.com/entry/123456/"
echo ""

read -p "Would you like to edit .env now? (y/n) " -n 1 -r
echo ""

if [[ $REPLY =~ ^[Yy]$ ]]; then
    ${EDITOR:-nano} .env
fi

echo ""
echo "=================================="
echo "Verifying Configuration"
echo "=================================="
echo ""

# Check if TELEGRAM_BOT_TOKEN is set
if grep -q "TELEGRAM_BOT_TOKEN=your_telegram_bot_token_here" .env; then
    echo "⚠️  WARNING: TELEGRAM_BOT_TOKEN not configured"
    echo "   The bot will not work without a valid token!"
    echo ""
    read -p "Continue anyway? (y/n) " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Please edit .env and run this script again."
        exit 1
    fi
else
    echo "✅ TELEGRAM_BOT_TOKEN configured"
fi

echo ""
echo "=================================="
echo "Building Docker Images"
echo "=================================="
echo ""

echo "This may take a few minutes on first run..."
docker compose build

echo ""
echo "✅ Docker images built successfully"
echo ""

echo "=================================="
echo "Creating Data Volume"
echo "=================================="
echo ""

docker compose up --no-start
echo "✅ Volume created"

echo ""
echo "=================================="
echo "Setup Complete!"
echo "=================================="
echo ""
echo "Next steps:"
echo ""
echo "1. Start the services:"
echo "   make up"
echo ""
echo "2. Check status:"
echo "   make status"
echo ""
echo "3. View logs:"
echo "   make logs"
echo ""
echo "4. Start using your Telegram bot:"
echo "   - Search for your bot on Telegram"
echo "   - Send /start to initialize"
echo "   - Send /suggest to get transfer recommendations"
echo "   - Send /report for top player reports"
echo ""
echo "For more information:"
echo "  - make help           # See all commands"
echo "  - cat DEPLOYMENT.md   # Deployment guide"
echo "  - cat docs/ARCHITECTURE.md  # Technical details"
echo ""
echo "=================================="
echo "Memory Usage Target"
echo "=================================="
echo ""
echo "Expected memory usage:"
echo "  - fpl-core: ~35MB (periodic)"
echo "  - fpl-bot:  ~42MB (persistent)"
echo "  - Total:    ~110MB peak"
echo ""
echo "Your server: 2GB RAM"
echo "Usage:       ~5.5%"
echo "Remaining:   ~1.9GB for other apps"
echo ""
echo "Happy scouting! ⚽"
