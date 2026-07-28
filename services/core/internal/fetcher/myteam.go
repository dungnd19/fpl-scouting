package fetcher

import (
	"fmt"
	"log"

	"fpl-core/internal/models"
)

const (
	entryURL = "https://fantasy.premierleague.com/api/entry/%s/"
	picksURL = "https://fantasy.premierleague.com/api/entry/%s/event/%d/picks/"
)

// FetchMyTeamPublic fetches the user's squad using the public API (no auth needed).
// Returns a MyTeam struct compatible with the existing storage.
func (s *Service) FetchMyTeamPublic(teamID string) (*models.MyTeam, error) {
	if teamID == "" {
		return nil, fmt.Errorf("team ID is required")
	}

	// Step 1: Get current gameweek from entry endpoint
	var entry models.EntryInfo
	if err := s.client.FetchJSON(fmt.Sprintf(entryURL, teamID), &entry); err != nil {
		return nil, fmt.Errorf("failed to fetch entry info: %w", err)
	}

	if entry.CurrentEvent == 0 {
		return nil, fmt.Errorf("no current gameweek found (season may not have started)")
	}

	log.Printf("Current gameweek: %d", entry.CurrentEvent)

	// Step 2: Get picks for current gameweek
	var publicPicks models.PublicPicks
	if err := s.client.FetchJSON(fmt.Sprintf(picksURL, teamID, entry.CurrentEvent), &publicPicks); err != nil {
		return nil, fmt.Errorf("failed to fetch picks for GW%d: %w", entry.CurrentEvent, err)
	}

	// Step 3: Convert to MyTeam format
	myTeam := &models.MyTeam{
		Picks: make([]models.TeamPick, 0, len(publicPicks.Picks)),
	}
	myTeam.Transfers.Bank = publicPicks.EntryHistory.Bank
	myTeam.Transfers.Value = publicPicks.EntryHistory.Value

	for _, p := range publicPicks.Picks {
		myTeam.Picks = append(myTeam.Picks, models.TeamPick{
			Element:       p.Element,
			Position:      p.Position,
			IsCaptain:     p.IsCaptain,
			IsViceCaptain: p.IsViceCaptain,
			Multiplier:    p.Multiplier,
			// No selling/purchase price from public API — will use current market price
			SellingPrice:  0,
			PurchasePrice: 0,
		})
	}

	return myTeam, nil
}
