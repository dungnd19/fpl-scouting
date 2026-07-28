package checker

import (
	"log"
	"time"

	"fpl-bot/internal/database"
	"fpl-bot/internal/telegram"
)

// Service handles periodic checking for new recommendations
type Service struct {
	repo     *database.Repository
	bot      *telegram.Bot
	interval time.Duration
}

// NewService creates a new checker service
func NewService(repo *database.Repository, bot *telegram.Bot, intervalMinutes int) *Service {
	return &Service{
		repo:     repo,
		bot:      bot,
		interval: time.Duration(intervalMinutes) * time.Minute,
	}
}

// Start starts the periodic checker
func (s *Service) Start() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	// Run immediately on start
	time.Sleep(5 * time.Second)
	s.checkAndNotify()

	for range ticker.C {
		s.checkAndNotify()
	}
}

// checkAndNotify checks for new recommendations and sends notifications
func (s *Service) checkAndNotify() {
	log.Println("Checking for new recommendations...")

	// Get pending recommendations
	recommendations, err := s.repo.GetPendingRecommendations(5)
	if err != nil {
		log.Printf("Failed to get recommendations: %v", err)
		return
	}

	if len(recommendations) == 0 {
		log.Println("No pending recommendations")
		return
	}

	log.Printf("Found %d pending recommendations", len(recommendations))

	// Send recommendations
	for _, rec := range recommendations {
		if err := s.bot.SendRecommendationToUser(rec); err != nil {
			log.Printf("Failed to send recommendation: %v", err)
		}
		time.Sleep(1 * time.Second) // Rate limiting
	}
}
