package telegram

import (
	"fmt"
	"strings"
	"time"

	"fpl-bot/internal/analyzer"
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

// FormatRecommendation formats a recommendation as a Telegram message
func FormatRecommendation(sellName, buyName string, expectedGain, priceDiff float64, reason string, score float64) string {
	return fmt.Sprintf(
		"📊 *Transfer Recommendation*\n\n"+
			"🔴 Sell: *%s*\n"+
			"🟢 Buy: *%s*\n\n"+
			"📈 Expected gain: *%.1f* points\n"+
			"💰 Price difference: *£%.1fm*\n\n"+
			"ℹ️ %s\n\n"+
			"Score: %.2f",
		sellName, buyName,
		expectedGain, priceDiff,
		reason, score,
	)
}

// FormatStatus formats system status as a Telegram message
func FormatStatus(playerCount, recCount int, lastFetch, lastAnalyze string) string {
	return fmt.Sprintf(
		"📊 *System Status*\n\n"+
			"👥 Players in DB: %d\n"+
			"📝 Pending recommendations: %d\n"+
			"🔄 Last fetch: %s\n"+
			"📊 Last analysis: %s",
		playerCount, recCount, FormatTime(lastFetch), FormatTime(lastAnalyze),
	)
}

// FormatTime formats a timestamp string for display
func FormatTime(timeStr string) string {
	if timeStr == "" {
		return "Never"
	}
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return timeStr
	}
	return t.Format("2006-01-02 15:04")
}

// FormatInitSquad formats an optimized 15-player squad for Telegram
func FormatInitSquad(squad *analyzer.OptimizedSquad) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("*Initial Squad* (%s)\n", squad.Formation()))
	b.WriteString(fmt.Sprintf("Budget: £%.1fM | Bank: £%.1fM | EP: %.1f\n\n",
		float64(squad.TotalCost)/10.0, float64(squad.Bank)/10.0, squad.Starting11EP()))

	formatGroup := func(label string, slots []analyzer.SquadSlot) {
		if len(slots) == 0 {
			return
		}
		b.WriteString(fmt.Sprintf("*%s*\n", label))
		for _, s := range slots {
			prefix := "  "
			if s.IsStarter {
				prefix = "▶ "
			} else {
				prefix = "  B "
			}
			b.WriteString(fmt.Sprintf("%s%-20s %-4s £%4.1fm  EP: %5.2f\n",
				prefix, s.Player.WebName, s.Player.TeamName,
				float64(s.Player.NowCost)/10.0, s.Player.OverallScore))
		}
		b.WriteString("\n")
	}

	formatGroup("GOALKEEPERS", squad.Goalkeepers)
	formatGroup("DEFENDERS", squad.Defenders)
	formatGroup("MIDFIELDERS", squad.Midfielders)
	formatGroup("FORWARDS", squad.Forwards)

	return b.String()
}

// WelcomeMessage returns the welcome message
func WelcomeMessage() string {
	return "Welcome to FPL Scouting Bot!\n\n" +
		"Commands:\n" +
		"/myteam - Show your current squad\n" +
		"/fetch - Trigger FPL data fetch on demand\n" +
		"/suggest - Analyze squad & suggest transfers\n" +
		"/startsquad - Build initial 15-player squad\n" +
		"/report - Top 5 per position (5/10 GW & season)\n" +
		"/recommendations - View pending recommendations\n" +
		"/status - System status"
}

// FormatMyTeam formats the user's squad for Telegram
func FormatMyTeam(players []database.MyTeamPlayer, bank int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("*Your Squad* (Bank: £%.1fm)\n", float64(bank)/10.0))

	// Group: starting XI (pos 1-11) and bench (12-15)
	lastPos := 0

	for i, p := range players {
		if i == 11 {
			b.WriteString("\n*Bench*\n")
			lastPos = 0
		}

		if p.Position != lastPos && i < 11 {
			b.WriteString(fmt.Sprintf("\n*%s*\n", models.PositionNames[p.Position]))
			lastPos = p.Position
		}

		captain := ""
		if p.IsCaptain {
			captain = " (C)"
		} else if p.IsViceCaptain {
			captain = " (V)"
		}

		b.WriteString(fmt.Sprintf("  %s%s — %s £%.1fm | %dpts, form %.1f\n",
			p.WebName, captain, p.TeamName,
			float64(p.NowCost)/10.0, p.TotalPoints, p.Form,
		))
	}

	// Total squad value
	total := bank
	for _, p := range players {
		total += p.SellingPrice
	}
	b.WriteString(fmt.Sprintf("\n*Total value:* £%.1fm", float64(total)/10.0))

	return b.String()
}

// FormatReport formats a top-5-per-position report for Telegram
func FormatReport(title string, reports []analyzer.PositionReport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("*%s*\n", title))

	for _, pr := range reports {
		posLabel := models.PositionNames[pr.Position]
		b.WriteString(fmt.Sprintf("\n*%s*\n", posLabel))

		for i, p := range pr.Players {
			line := formatReportLine(i+1, p)
			b.WriteString(line)
		}
	}

	return b.String()
}

// formatReportLine formats a single player line in a report
func formatReportLine(rank int, p models.PlayerReport) string {
	price := float64(p.NowCost) / 10.0
	return fmt.Sprintf(
		"%d. %s (%s) £%.1fm | xS:%.1f ppg:%.1f xGI:%.2f\n",
		rank, p.WebName, p.TeamName, price,
		p.XScore, p.PPG, p.XGIPer90,
	)
}
