package telegram

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"fpl-bot/internal/analyzer"
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
	"fpl-bot/internal/trader"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Bot wraps the Telegram bot functionality
type Bot struct {
	api      *tgbotapi.BotAPI
	repo     *database.Repository
	trader   *trader.Service
	analyzer *analyzer.Service
	userID   string
}

// NewBot creates a new Telegram bot
func NewBot(token string, repo *database.Repository, traderSvc *trader.Service, analyzerSvc *analyzer.Service, userID string, debug bool) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	api.Debug = debug
	log.Printf("Authorized on account %s", api.Self.UserName)

	return &Bot{
		api:      api,
		repo:     repo,
		trader:   traderSvc,
		analyzer: analyzerSvc,
		userID:   userID,
	}, nil
}

// Start starts the bot update loop
func (b *Bot) Start() error {
	// Register command menu for auto-complete
	commands := []tgbotapi.BotCommand{
		{Command: "myteam", Description: "Show your current squad"},
		{Command: "fetch", Description: "Trigger FPL data fetch on demand"},
		{Command: "suggest", Description: "Analyze squad & suggest transfers"},
		{Command: "init_squad", Description: "Build initial 15-player squad"},
		{Command: "report", Description: "Top 5 per position (5/10 GW & season)"},
		{Command: "recommendations", Description: "View pending recommendations"},
		{Command: "status", Description: "System status"},
	}
	cmdCfg := tgbotapi.NewSetMyCommands(commands...)
	if _, err := b.api.Request(cmdCfg); err != nil {
		log.Printf("Warning: failed to set command menu: %v", err)
	} else {
		log.Println("Command menu registered")
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	log.Println("Bot is ready to receive updates")
	for update := range updates {
		if update.Message != nil {
			b.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			b.handleCallback(update.CallbackQuery)
		}
	}

	return nil
}

// handleMessage handles incoming messages
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	log.Printf("[%s] %s", message.From.UserName, message.Text)

	// Register user's chat ID
	if err := b.repo.RegisterChatID(b.userID, message.Chat.ID); err != nil {
		log.Printf("Failed to register chat ID: %v", err)
	}

	// Handle commands (longer prefixes first to avoid partial match, e.g. /status vs /start)
	switch {
	case strings.HasPrefix(message.Text, "/init_squad"):
		b.handleStartSquad(message)
	case strings.HasPrefix(message.Text, "/status"):
		b.handleStatus(message)
	case strings.HasPrefix(message.Text, "/recommendations"):
		b.handleRecommendations(message)
	case strings.HasPrefix(message.Text, "/myteam"):
		b.handleMyTeam(message)
	case strings.HasPrefix(message.Text, "/fetch"):
		b.handleFetch(message)
	case strings.HasPrefix(message.Text, "/suggest"):
		b.handleSuggest(message)
	case strings.HasPrefix(message.Text, "/report"):
		b.handleReport(message)
	case strings.HasPrefix(message.Text, "/start"):
		b.handleStart(message)
	default:
		msg := tgbotapi.NewMessage(message.Chat.ID, "Unknown command. Use /start to see available commands.")
		b.api.Send(msg)
	}
}

// handleStart handles the /start command
func (b *Bot) handleStart(message *tgbotapi.Message) {
	msg := tgbotapi.NewMessage(message.Chat.ID, WelcomeMessage())
	b.api.Send(msg)
}

// handleStatus handles the /status command
func (b *Bot) handleStatus(message *tgbotapi.Message) {
	status, err := b.repo.GetSystemStatus()
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Failed to get system status")
		b.api.Send(msg)
		return
	}

	text := FormatStatus(
		status["player_count"].(int),
		status["pending_recommendations"].(int),
		status["last_fetch"].(string),
		status["last_analyze"].(string),
	)

	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "Markdown"
	b.api.Send(msg)
}

// handleMyTeam shows the user's current squad
func (b *Bot) handleMyTeam(message *tgbotapi.Message) {
	players, err := b.repo.GetMyTeamFull(b.userID)
	if err != nil || len(players) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "No squad data found. Wait for the next data fetch (runs every hour).")
		b.api.Send(msg)
		return
	}

	bank, _ := b.repo.GetBank()
	text := FormatMyTeam(players, bank)
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "Markdown"
	b.api.Send(msg)
}

// handleFetch triggers a data fetch on the core service
func (b *Bot) handleFetch(message *tgbotapi.Message) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://fpl-core:8090/fetch")
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Failed to trigger fetch: %v", err))
		b.api.Send(msg)
		return
	}
	defer resp.Body.Close()

	msg := tgbotapi.NewMessage(message.Chat.ID, "Fetch triggered. Data will be refreshed shortly.")
	b.api.Send(msg)
}

// parseSuggestHorizon extracts the optional gameweek-lookahead argument from
// "/suggest [1|2|3]", defaulting to 3 when absent.
func parseSuggestHorizon(text string) (int, error) {
	arg := strings.TrimSpace(strings.TrimPrefix(text, "/suggest"))
	if arg == "" {
		return 3, nil
	}
	n, err := strconv.Atoi(arg)
	if err != nil {
		return 0, fmt.Errorf("gameweek horizon must be a number (1-3), got %q", arg)
	}
	if n < 1 || n > 3 {
		return 0, fmt.Errorf("gameweek horizon must be between 1 and 3, got %d", n)
	}
	return n, nil
}

// handleSuggest runs on-demand analysis and sends transfer recommendations
func (b *Bot) handleSuggest(message *tgbotapi.Message) {
	horizon, err := parseSuggestHorizon(message.Text)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("%s. Usage: /suggest [1|2|3]", err.Error()))
		b.api.Send(msg)
		return
	}

	// Send "thinking" message
	thinking := tgbotapi.NewMessage(message.Chat.ID, "Analyzing your squad against all players (xG/xA/xCS)...")
	b.api.Send(thinking)

	recs, err := b.analyzer.Suggest(horizon)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Analysis failed: %s", err.Error()))
		b.api.Send(msg)
		return
	}

	if len(recs) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "No transfer improvements found for your current squad.")
		b.api.Send(msg)
		return
	}

	for _, rec := range recs {
		b.sendRecommendation(message.Chat.ID, rec)
	}
}

// handleStartSquad handles /init_squad — builds and sends an initial 15-player squad.
func (b *Bot) handleStartSquad(message *tgbotapi.Message) {
	thinking := tgbotapi.NewMessage(message.Chat.ID, "Building initial squad (this may take a minute)...")
	b.api.Send(thinking)

	squad, err := b.analyzer.BuildInitialSquad()
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, fmt.Sprintf("Squad build failed: %s", err.Error()))
		b.api.Send(msg)
		return
	}

	text := FormatInitSquad(squad)
	msg := tgbotapi.NewMessage(message.Chat.ID, text)
	msg.ParseMode = "Markdown"
	if _, err := b.api.Send(msg); err != nil {
		log.Printf("Failed to send squad: %v", err)
	}
}

// handleReport sends top-5-per-position reports for different game windows
func (b *Bot) handleReport(message *tgbotapi.Message) {
	thinking := tgbotapi.NewMessage(message.Chat.ID, "Generating reports...")
	b.api.Send(thinking)

	windows := []struct {
		n     int
		label string
	}{
		{5, "Last 5 Gameweeks"},
		{10, "Last 10 Gameweeks"},
		{0, "Full Season"},
	}

	for _, w := range windows {
		reports, err := b.analyzer.Report(w.n)
		if err != nil {
			log.Printf("Report failed for window %d: %v", w.n, err)
			continue
		}

		text := FormatReport(w.label, reports)
		msg := tgbotapi.NewMessage(message.Chat.ID, text)
		msg.ParseMode = "Markdown"
		if _, err := b.api.Send(msg); err != nil {
			log.Printf("Failed to send report: %v", err)
		}
	}
}

// handleRecommendations handles the /recommendations command
func (b *Bot) handleRecommendations(message *tgbotapi.Message) {
	recommendations, err := b.repo.GetPendingRecommendations(5)
	if err != nil {
		msg := tgbotapi.NewMessage(message.Chat.ID, "Failed to get recommendations")
		b.api.Send(msg)
		return
	}

	if len(recommendations) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, "No pending recommendations at the moment.")
		b.api.Send(msg)
		return
	}

	for _, rec := range recommendations {
		b.sendRecommendation(message.Chat.ID, rec)
	}
}

// sendRecommendation sends a recommendation to a chat
func (b *Bot) sendRecommendation(chatID int64, rec models.Recommendation) {
	text := FormatRecommendation(
		rec.SellPlayerName, rec.BuyPlayerName,
		rec.ExpectedGain, rec.PriceDiff,
		rec.Reason, rec.Score,
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"

	// Add inline keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Confirm Transfer", fmt.Sprintf("confirm:%d", rec.ID)),
			tgbotapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("reject:%d", rec.ID)),
		),
	)
	msg.ReplyMarkup = keyboard

	if _, err := b.api.Send(msg); err != nil {
		log.Printf("Failed to send message: %v", err)
		return
	}

	// Mark as sent
	if err := b.repo.UpdateRecommendationStatus(rec.ID, "sent"); err != nil {
		log.Printf("Failed to update recommendation status: %v", err)
	}
}

// handleCallback handles callback queries from inline keyboards
func (b *Bot) handleCallback(callback *tgbotapi.CallbackQuery) {
	parts := strings.Split(callback.Data, ":")
	if len(parts) != 2 {
		return
	}

	action := parts[0]
	recID, err := strconv.Atoi(parts[1])
	if err != nil {
		return
	}

	switch action {
	case "confirm":
		b.handleConfirm(callback, recID)
	case "reject":
		b.handleReject(callback, recID)
	}
}

// handleConfirm handles transfer confirmation
func (b *Bot) handleConfirm(callback *tgbotapi.CallbackQuery, recID int) {
	log.Printf("Transfer confirmed for recommendation %d", recID)

	// Get recommendation details
	rec, err := b.repo.GetRecommendation(recID)
	if err != nil {
		log.Printf("Failed to get recommendation: %v", err)
		return
	}

	// Execute transfer
	result := b.trader.ExecuteTransfer(rec)

	// Update recommendation status
	status := "executed"
	if !result.Success {
		status = "failed"
	}
	b.repo.UpdateRecommendationStatus(recID, status)

	// Send response
	response := fmt.Sprintf("✅ Transfer confirmed!\n\nSell: %s\nBuy: %s\n\n", rec.SellPlayerName, rec.BuyPlayerName)
	if result.Success {
		response += "✅ Transfer executed successfully!"
	} else {
		response += fmt.Sprintf("❌ Transfer failed: %s", result.Error)
	}

	edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, response)
	b.api.Send(edit)

	answerCallback := tgbotapi.NewCallback(callback.ID, "Transfer confirmed")
	b.api.Send(answerCallback)
}

// handleReject handles transfer rejection
func (b *Bot) handleReject(callback *tgbotapi.CallbackQuery, recID int) {
	log.Printf("Transfer rejected for recommendation %d", recID)

	b.repo.UpdateRecommendationStatus(recID, "rejected")

	response := "❌ Transfer rejected"
	edit := tgbotapi.NewEditMessageText(callback.Message.Chat.ID, callback.Message.MessageID, response)
	b.api.Send(edit)

	answerCallback := tgbotapi.NewCallback(callback.ID, "Transfer rejected")
	b.api.Send(answerCallback)
}

// SendRecommendationToUser sends a recommendation to the default user
func (b *Bot) SendRecommendationToUser(rec models.Recommendation) error {
	chatID, err := b.repo.GetUserChatID(b.userID)
	if err != nil || chatID == 0 {
		return fmt.Errorf("no chat ID registered for user %s", b.userID)
	}

	b.sendRecommendation(chatID, rec)
	return nil
}
