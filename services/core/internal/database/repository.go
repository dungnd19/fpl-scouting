package database

import (
	"fmt"
	"log"
	"strings"
	"time"

	"fpl-core/internal/models"
)

// Repository provides database operations
type Repository struct {
	db *DB
}

// NewRepository creates a new repository
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// StoreTeams stores teams in the database
func (r *Repository) StoreTeams(teams []models.Team) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO teams (
			id, code, name, short_name, strength,
			strength_overall_home, strength_overall_away,
			strength_attack_home, strength_attack_away,
			strength_defence_home, strength_defence_away,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, team := range teams {
		_, err := stmt.Exec(
			team.ID, team.Code, team.Name, team.ShortName, team.Strength,
			team.StrengthOverallHome, team.StrengthOverallAway,
			team.StrengthAttackHome, team.StrengthAttackAway,
			team.StrengthDefenceHome, team.StrengthDefenceAway,
		)
		if err != nil {
			return fmt.Errorf("failed to insert team %d: %w", team.ID, err)
		}
	}

	return tx.Commit()
}

// StorePlayers stores players in the database
func (r *Repository) StorePlayers(players []models.Player) error {
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO players (
			id, code, first_name, second_name, web_name, team, element_type,
			now_cost, form, points_per_game, selected_by_percent, total_points,
			minutes, goals_scored, assists, clean_sheets, goals_conceded, saves,
			bonus, bps, influence, creativity, threat, ict_index,
			expected_goals, expected_assists, expected_goal_involvements,
			expected_goals_conceded, status, chance_of_playing_next_round,
			updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, p := range players {
		_, err := stmt.Exec(
			p.ID, p.Code, p.FirstName, p.SecondName, p.WebName, p.Team, p.ElementType,
			p.NowCost, p.Form, p.PointsPerGame, p.SelectedByPercent, p.TotalPoints,
			p.Minutes, p.GoalsScored, p.Assists, p.CleanSheets, p.GoalsConceded, p.Saves,
			p.Bonus, p.BPS, p.Influence, p.Creativity, p.Threat, p.ICTIndex,
			p.ExpectedGoals, p.ExpectedAssists, p.ExpectedGoalInvolvements,
			p.ExpectedGoalsConceded, p.Status, p.ChanceOfPlayingNextRound,
		)
		if err != nil {
			return fmt.Errorf("failed to insert player %d: %w", p.ID, err)
		}
	}

	return tx.Commit()
}

// StorePlayerHistory stores player history entries
func (r *Repository) StorePlayerHistory(history []models.HistoryEntry) error {
	if len(history) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO player_history (
			player_id, event, fixture, opponent_team, total_points, was_home,
			kickoff_time, minutes, goals_scored, assists, clean_sheets,
			goals_conceded, saves, bonus, bps, influence, creativity, threat,
			ict_index, value, selected, transfers_balance, transfers_in,
			transfers_out, expected_goals, expected_assists,
			expected_goal_involvements, expected_goals_conceded,
			clearances_blocks_interceptions, tackles, recoveries
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, h := range history {
		_, err := stmt.Exec(
			h.Element, h.Round, h.Fixture, h.OpponentTeam, h.TotalPoints, h.WasHome,
			h.KickoffTime, h.Minutes, h.GoalsScored, h.Assists, h.CleanSheets,
			h.GoalsConceded, h.Saves, h.Bonus, h.BPS, h.Influence, h.Creativity, h.Threat,
			h.ICTIndex, h.Value, h.Selected, h.TransfersBalance, h.TransfersIn,
			h.TransfersOut, h.ExpectedGoals, h.ExpectedAssists,
			h.ExpectedGoalInvolvements, h.ExpectedGoalsConceded,
			h.ClearancesBlocksInterceptions, h.Tackles, h.Recoveries,
		)
		if err != nil {
			return fmt.Errorf("failed to insert history for player %d: %w", h.Element, err)
		}
	}

	return tx.Commit()
}

// UpdateMetadata updates a metadata key-value pair
func (r *Repository) UpdateMetadata(key, value string) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, key, value)
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}
	return nil
}

// SetLastFetch updates the last fetch timestamp
func (r *Repository) SetLastFetch() error {
	return r.UpdateMetadata("last_fetch", time.Now().Format(time.RFC3339))
}

// StoreSeasonHistoryBatch stores historical season data from vaastav fetcher.
func (r *Repository) StoreSeasonHistoryBatch(entries []models.SeasonHistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO season_history (
			season, player_name, team_name, position, event,
			minutes, total_points, goals_scored, assists, clean_sheets,
			goals_conceded, expected_goals, expected_assists,
			expected_goal_involvements, expected_goals_conceded,
			bonus, bps, influence, creativity, threat, ict_index,
			was_home, opponent_team, kickoff_time, value, selected
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	batchSize := 500
	for i := 0; i < len(entries); i += batchSize {
		end := i + batchSize
		if end > len(entries) {
			end = len(entries)
		}
		for _, e := range entries[i:end] {
			pos, _ := positionStrToInt(e.Position)
			_, err := stmt.Exec(
				e.Season, e.PlayerName, e.TeamName, pos, e.Event,
				e.Minutes, e.TotalPoints, e.GoalsScored, e.Assists, e.CleanSheets,
				e.GoalsConceded, e.ExpectedGoals, e.ExpectedAssists,
				e.ExpectedGoalInvolvements, e.ExpectedGoalsConceded,
				e.Bonus, e.BPS, e.Influence, e.Creativity, e.Threat, e.ICTIndex,
				e.WasHome, e.OpponentTeam, e.KickoffTime, e.Value, e.Selected,
			)
			if err != nil {
				return fmt.Errorf("failed to insert season history: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	log.Printf("Stored %d season history entries", len(entries))
	return nil
}

func positionStrToInt(pos string) (int, error) {
	switch pos {
	case "GK", "GKP":
		return 1, nil
	case "DEF":
		return 2, nil
	case "MID":
		return 3, nil
	case "FWD":
		return 4, nil
	default:
		return 0, fmt.Errorf("unknown position: %s", pos)
	}
}

// BuildCrossSeasonMap matches current players to historical player names.
// Uses exact name match on web_name / second_name / full name.
func (r *Repository) BuildCrossSeasonMap(season string) error {
	rows, err := r.db.Query(`
		SELECT id, web_name, second_name, first_name
		FROM players WHERE minutes > 0
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type currentPlayer struct {
		id         int
		webName    string
		secondName string
		firstName  string
	}
	var players []currentPlayer
	for rows.Next() {
		var p currentPlayer
		if err := rows.Scan(&p.id, &p.webName, &p.secondName, &p.firstName); err != nil {
			return err
		}
		players = append(players, p)
	}

	// Get unique player names from the season history
	nameRows, err := r.db.Query(`
		SELECT DISTINCT player_name FROM season_history WHERE season = ?
	`, season)
	if err != nil {
		return err
	}
	defer nameRows.Close()

	histNames := make(map[string]bool)
	for nameRows.Next() {
		var name string
		if err := nameRows.Scan(&name); err != nil {
			return err
		}
		histNames[strings.ToLower(name)] = true
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO cross_season_map (current_player_id, season, prior_player_name, confidence)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range players {
		fullName := strings.ToLower(p.firstName + " " + p.secondName)
		lowerWeb := strings.ToLower(p.webName)

		if histNames[fullName] {
			stmt.Exec(p.id, season, p.firstName+" "+p.secondName, 1.0)
		} else if histNames[lowerWeb] {
			stmt.Exec(p.id, season, p.webName, 0.9)
		}
	}

	return tx.Commit()
}
