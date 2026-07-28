package database

import (
	"fmt"
	"log"

	"fpl-core/internal/models"
)

// StoreMyTeam stores the user's current FPL team and transfer budget info
func (r *Repository) StoreMyTeam(userID string, team *models.MyTeam) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing team data for this user
	_, err = tx.Exec("DELETE FROM my_team WHERE user_id = ?", userID)
	if err != nil {
		return fmt.Errorf("failed to clear old team: %w", err)
	}

	// Insert current team
	stmt, err := tx.Prepare(`
		INSERT INTO my_team (
			user_id, player_id, position, is_captain, is_vice_captain,
			multiplier, selling_price, purchase_price
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, pick := range team.Picks {
		_, err := stmt.Exec(
			userID,
			pick.Element,
			pick.Position,
			boolToInt(pick.IsCaptain),
			boolToInt(pick.IsViceCaptain),
			pick.Multiplier,
			pick.SellingPrice,
			pick.PurchasePrice,
		)
		if err != nil {
			return fmt.Errorf("failed to insert team pick: %w", err)
		}
	}

	// Store bank balance and squad value in metadata
	if _, err := tx.Exec(`INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES ('bank', ?, CURRENT_TIMESTAMP)`,
		fmt.Sprintf("%d", team.Transfers.Bank)); err != nil {
		log.Printf("Warning: failed to store bank: %v", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO metadata (key, value, updated_at) VALUES ('squad_value', ?, CURRENT_TIMESTAMP)`,
		fmt.Sprintf("%d", team.Transfers.Value)); err != nil {
		log.Printf("Warning: failed to store squad value: %v", err)
	}

	// Backfill selling_price with current market price where missing (public API doesn't provide it)
	if _, err := tx.Exec(`
		UPDATE my_team SET selling_price = (SELECT now_cost FROM players WHERE id = my_team.player_id)
		WHERE user_id = ? AND (selling_price = 0 OR selling_price IS NULL)
	`, userID); err != nil {
		log.Printf("Warning: failed to backfill selling prices: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Stored team for user %s: %d players, bank: £%.1fm, free transfers: %d",
		userID, len(team.Picks), float64(team.Transfers.Bank)/10.0,
		team.Transfers.Limit-team.Transfers.Made)
	return nil
}

// GetMyTeamPlayerIDs returns a map of player IDs in the user's team
func (r *Repository) GetMyTeamPlayerIDs(userID string) (map[int]bool, error) {
	rows, err := r.db.Query("SELECT player_id FROM my_team WHERE user_id = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query my_team: %w", err)
	}
	defer rows.Close()

	playerIDs := make(map[int]bool)
	for rows.Next() {
		var playerID int
		if err := rows.Scan(&playerID); err != nil {
			return nil, fmt.Errorf("failed to scan player_id: %w", err)
		}
		playerIDs[playerID] = true
	}

	return playerIDs, rows.Err()
}

// CleanOldRecommendations deletes old recommendations (older than N days)
func (r *Repository) CleanOldRecommendations(daysToKeep int) error {
	result, err := r.db.Exec(`
		DELETE FROM recommendations 
		WHERE timestamp < datetime('now', '-' || ? || ' days')
		AND status IN ('sent', 'rejected', 'executed', 'failed')
	`, daysToKeep)
	
	if err != nil {
		return fmt.Errorf("failed to clean old recommendations: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows > 0 {
		log.Printf("Cleaned %d old recommendations", rows)
	}

	return nil
}

// boolToInt converts boolean to integer (0 or 1)
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
