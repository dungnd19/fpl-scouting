package analyzer

import (
	"fmt"
	"math"

	"fpl-core/internal/models"
)

// ScorePlayer calculates various metrics for a player
func ScorePlayer(p models.PlayerScore, recentForm float64) models.PlayerScore {
	// Use recent form if available, otherwise use season form
	if recentForm > 0 {
		p.RecentForm = recentForm
	} else {
		p.RecentForm = p.Form
	}

	// Expected points (weighted average of form and recent performance)
	// Recent form is weighted more heavily (60%) than season average (40%)
	p.ExpectedPoints = (p.PointsPerGame * 0.4) + (p.RecentForm * 0.6)

	// Value score (points per million)
	costInMillions := float64(p.NowCost) / 10.0
	if costInMillions > 0 {
		p.Value = p.ExpectedPoints / costInMillions
	}

	// Overall score (composite metric)
	p.OverallScore = calculateOverallScore(p)

	return p
}

// calculateOverallScore computes a composite score for a player
// Weighted scoring:
// - Expected points: 40%
// - Value: 30%
// - Form: 20%
// - Availability: 10%
func calculateOverallScore(p models.PlayerScore) float64 {
	score := 0.0
	score += p.ExpectedPoints * 4.0
	score += p.Value * 3.0
	score += p.Form * 2.0
	score += p.Availability * 10.0
	return score
}

// GenerateRecommendation creates a transfer recommendation
func GenerateRecommendation(sell, buy models.PlayerScore) *models.TransferRecommendation {
	// Calculate expected gain
	gain := buy.ExpectedPoints - sell.ExpectedPoints
	if gain <= 0 {
		return nil
	}

	// Don't recommend same team (FPL rules: max 3 from same team)
	if sell.Team == buy.Team {
		return nil
	}

	priceDiff := float64(buy.NowCost-sell.NowCost) / 10.0

	// Build reason
	reason := formatReason(sell, buy, gain)

	// Score the recommendation (gain minus price penalty)
	recScore := gain - (math.Abs(priceDiff) * 0.5)

	return &models.TransferRecommendation{
		SellPlayerID:   sell.PlayerID,
		BuyPlayerID:    buy.PlayerID,
		SellPlayerName: sell.WebName,
		BuyPlayerName:  buy.WebName,
		ExpectedGain:   gain,
		PriceDiff:      priceDiff,
		Reason:         reason,
		Score:          recScore,
	}
}

// formatReason creates a human-readable reason for the recommendation
func formatReason(sell, buy models.PlayerScore, gain float64) string {
	return fmt.Sprintf(
		"%s (%.1f pts/game, form %.1f) → %s (%.1f pts/game, form %.1f). Expected gain: +%.1f pts",
		sell.WebName, sell.PointsPerGame, sell.Form,
		buy.WebName, buy.PointsPerGame, buy.Form,
		gain,
	)
}
