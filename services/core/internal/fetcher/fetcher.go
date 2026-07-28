package fetcher

import (
	"fmt"
	"log"
	"time"

	"fpl-core/internal/database"
	"fpl-core/internal/models"
)

const (
	bootstrapURL = "https://fantasy.premierleague.com/api/bootstrap-static/"
	elementURL   = "https://fantasy.premierleague.com/api/element-summary/%d/"
)

// Service handles data fetching from FPL API
type Service struct {
	client *Client
	repo   *database.Repository
}

// NewService creates a new fetcher service
func NewService(client *Client, repo *database.Repository) *Service {
	return &Service{
		client: client,
		repo:   repo,
	}
}

// FetchAll fetches all data from FPL API and stores it
func (s *Service) FetchAll() error {
	log.Println("Fetching bootstrap data...")

	// Fetch bootstrap data
	var bootstrap models.BootstrapData
	if err := s.client.FetchJSON(bootstrapURL, &bootstrap); err != nil {
		return fmt.Errorf("failed to fetch bootstrap: %w", err)
	}

	log.Printf("Fetched %d teams, %d players", len(bootstrap.Teams), len(bootstrap.Elements))

	// Store teams
	if err := s.repo.StoreTeams(bootstrap.Teams); err != nil {
		return fmt.Errorf("failed to store teams: %w", err)
	}
	log.Printf("Stored %d teams", len(bootstrap.Teams))

	// Store players
	if err := s.repo.StorePlayers(bootstrap.Elements); err != nil {
		return fmt.Errorf("failed to store players: %w", err)
	}
	log.Printf("Stored %d players", len(bootstrap.Elements))

	// Fetch detailed history for active players (>100 minutes)
	activePlayers := make([]int, 0)
	for _, p := range bootstrap.Elements {
		if p.Minutes > 100 {
			activePlayers = append(activePlayers, p.ID)
		}
	}

	log.Printf("Fetching history for %d active players...", len(activePlayers))
	if err := s.fetchPlayerHistory(activePlayers); err != nil {
		return fmt.Errorf("failed to fetch player history: %w", err)
	}

	// Update metadata
	if err := s.repo.SetLastFetch(); err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	log.Println("Fetch completed successfully")
	return nil
}

// FetchAllWithTeam fetches all data including user's team via the public API.
func (s *Service) FetchAllWithTeam(userID, teamID string) error {
	// First fetch all general data
	if err := s.FetchAll(); err != nil {
		return err
	}

	if teamID == "" {
		return nil
	}

	log.Printf("Fetching team for user %s (public API)...", userID)
	myTeam, err := s.FetchMyTeamPublic(teamID)
	if err != nil {
		log.Printf("Warning: failed to fetch user team: %v", err)
		return nil
	}

	if err := s.repo.StoreMyTeam(userID, myTeam); err != nil {
		log.Printf("Warning: failed to store user team: %v", err)
		return nil
	}

	log.Printf("Successfully fetched and stored team: %d players, bank: £%.1fm",
		len(myTeam.Picks), float64(myTeam.Transfers.Bank)/10.0)
	return nil
}

// fetchPlayerHistory fetches and stores history for given player IDs
func (s *Service) fetchPlayerHistory(playerIDs []int) error {
	for i, playerID := range playerIDs {
		// Progress logging
		if i > 0 && i%10 == 0 {
			log.Printf("Progress: %d/%d players", i, len(playerIDs))
			time.Sleep(1 * time.Second) // Rate limiting
		}

		url := fmt.Sprintf(elementURL, playerID)
		var summary models.ElementSummary
		if err := s.client.FetchJSON(url, &summary); err != nil {
			log.Printf("Warning: failed to fetch history for player %d: %v", playerID, err)
			continue
		}

		if err := s.repo.StorePlayerHistory(summary.History); err != nil {
			log.Printf("Warning: failed to store history for player %d: %v", playerID, err)
			continue
		}
	}

	log.Printf("Fetched history for %d players", len(playerIDs))
	return nil
}
