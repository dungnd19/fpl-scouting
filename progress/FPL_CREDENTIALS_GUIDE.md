# Getting Your FPL Credentials

To enable personalized transfer recommendations based on YOUR team, you need to provide:

## 1. FPL Team ID

1. Go to https://fantasy.premierleague.com/
2. Log in to your account
3. Click on "Points" or "Transfers"
4. Look at the URL - it will be something like: `https://fantasy.premierleague.com/entry/123456/event/20`
5. Your Team ID is the number after `/entry/` (e.g., `123456`)

## 2. FPL Session Cookie

1. Open https://fantasy.premierleague.com/ in your browser
2. Log in to your account
3. Open Developer Tools (F12 or Right-click → Inspect)
4. Go to the "Application" or "Storage" tab
5. Click on "Cookies" → "https://fantasy.premierleague.com"
6. Find the cookie named `pl_profile`
7. Copy its **Value** (it's a long string)

## 3. Update Your `.env` File

```bash
# Edit your .env file
nano .env  # or use your preferred editor
```

Add these lines:
```
FPL_TEAM_ID=your_team_id_here
FPL_SESSION_COOKIE=your_cookie_value_here
```

## 4. Rebuild and Restart

```bash
make down
make build
make up
```

## How It Works

Once configured, the system will:
- ✅ Fetch YOUR current team from FPL
- ✅ Analyze only YOUR players to find underperformers
- ✅ Suggest replacements that fit your budget
- ✅ Avoid recommending players already in your team
- ✅ Send personalized recommendations via Telegram

## Telegram Bot Setup

1. Search for `@BotFather` on Telegram
2. Your bot is already created: `nemo_fpl_scouting_bot`
3. Send `/start` to your bot to register your chat
4. You'll start receiving transfer recommendations!

## Testing

```bash
# Check logs
make logs

# Manually trigger analysis
make analyze

# Check status
make status
```
