package analyzer

import (
	"fmt"
	"log"
	"sort"

	"fpl-core/internal/database"
	"fpl-core/internal/models"
)

// Service handles player analysis and recommendation generation
type Service struct {
	repo *database.Repository
}

// NewService creates a new analyzer service
func NewService(repo *database.Repository) *Service {
	return &Service{
		repo: repo,
	}
}

// Analyze performs analysis and generates transfer recommendations based on user's team
func (s *Service) Analyze(userID, teamID, sessionCookie string) error {
	log.Println("Starting analysis...")

	// Clean old recommendations (keep last 7 days)
	if err := s.repo.CleanOldRecommendations(7); err != nil {
		log.Printf("Warning: failed to clean old recommendations: %v", err)
	}

	// Get user's current team player IDs from database
	myTeamPlayerIDs, err := s.repo.GetMyTeamPlayerIDs(userID)
	if err != nil {
		log.Printf("Warning: failed to get user team: %v", err)
		myTeamPlayerIDs = nil
	}

	if len(myTeamPlayerIDs) > 0 {
		log.Printf("Analyzing based on user's team: %d players", len(myTeamPlayerIDs))
	} else {
		log.Println("No team data found - generating general recommendations")
	}

	// Get all active players (with >90 minutes played)
	players, err := s.repo.GetActivePlayers(90)
	if err != nil {
		return fmt.Errorf("failed to get active players: %w", err)
	}

	log.Printf("Analyzing %d players...", len(players))

	// Score each player
	scoredPlayers := make([]models.PlayerScore, 0, len(players))
	for _, p := range players {
		// Get recent form (last 5 games)
		recentForm, err := s.repo.GetPlayerRecentForm(p.PlayerID, 5)
		if err != nil {
			log.Printf("Warning: failed to get recent form for player %d: %v", p.PlayerID, err)
			recentForm = 0
		}

		// Score the player
		scored := ScorePlayer(p, recentForm)
		scoredPlayers = append(scoredPlayers, scored)
	}

	// Sort by overall score
	sort.Slice(scoredPlayers, func(i, j int) bool {
		return scoredPlayers[i].OverallScore > scoredPlayers[j].OverallScore
	})

	// Generate transfer recommendations
	// If we have team info, filter to only players in user's team for selling
	recommendations := s.generateRecommendations(scoredPlayers, myTeamPlayerIDs)

	log.Printf("Generated %d recommendations", len(recommendations))

	// Store recommendations
	if err := s.repo.StoreRecommendations(userID, recommendations); err != nil {
		return fmt.Errorf("failed to store recommendations: %w", err)
	}

	// Update metadata
	if err := s.repo.SetLastAnalyze(); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Println("Analysis completed successfully")
	return nil
}

// generateRecommendations creates transfer recommendations by position
// If myTeamPlayerIDs is provided, only suggest selling players from that set
func (s *Service) generateRecommendations(players []models.PlayerScore, myTeamPlayerIDs map[int]bool) []models.TransferRecommendation {
	recommendations := make([]models.TransferRecommendation, 0)

	// Group players by position
	byPosition := make(map[int][]models.PlayerScore)
	for _, p := range players {
		byPosition[p.Position] = append(byPosition[p.Position], p)
	}

	// For each position, find underperforming players and suggest replacements
	for _, posList := range byPosition {
		if len(posList) < 2 {
			continue
		}

		// Sort by score
		sort.Slice(posList, func(i, j int) bool {
			return posList[i].OverallScore > posList[j].OverallScore
		})

		// Determine which players to consider for selling
		var bottomPlayers []models.PlayerScore
		if myTeamPlayerIDs != nil && len(myTeamPlayerIDs) > 0 {
			// Only consider players in user's team for selling
			for _, p := range posList {
				if myTeamPlayerIDs[p.PlayerID] {
					bottomPlayers = append(bottomPlayers, p)
				}
			}
			// Sort by score to get worst performers in user's team
			sort.Slice(bottomPlayers, func(i, j int) bool {
				return bottomPlayers[i].OverallScore < bottomPlayers[j].OverallScore
			})
			// Take worst 2-3 from user's team
			if len(bottomPlayers) > 3 {
				bottomPlayers = bottomPlayers[:3]
			}
		} else {
			// No team info - use bottom 20% of all players
			bottomCount := max(1, len(posList)/5)
			bottomPlayers = posList[len(posList)-bottomCount:]
		}

		// Get top players (potential buys) - exclude user's team if known
		topCount := max(1, len(posList)/5)
		topPlayers := make([]models.PlayerScore, 0)
		for _, p := range posList[:min(topCount*2, len(posList))] {
			// Skip if already in user's team
			if myTeamPlayerIDs != nil && myTeamPlayerIDs[p.PlayerID] {
				continue
			}
			topPlayers = append(topPlayers, p)
			if len(topPlayers) >= topCount {
				break
			}
		}

		// Generate recommendations for this position
		posRecs := s.generatePositionRecommendations(bottomPlayers, topPlayers)
		recommendations = append(recommendations, posRecs...)
	}

	// Sort all recommendations by score and keep top 5
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	if len(recommendations) > 5 {
		recommendations = recommendations[:5]
	}

	return recommendations
}

// generatePositionRecommendations generates recommendations for a specific position
func (s *Service) generatePositionRecommendations(bottom, top []models.PlayerScore) []models.TransferRecommendation {
	recommendations := make([]models.TransferRecommendation, 0)

	for _, sell := range bottom {
		for _, buy := range top {
			rec := GenerateRecommendation(sell, buy)
			if rec != nil {
				recommendations = append(recommendations, *rec)
			}
		}
	}

	// Keep only top 2 recommendations for this position
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Score > recommendations[j].Score
	})

	if len(recommendations) > 2 {
		recommendations = recommendations[:2]
	}

	return recommendations
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
