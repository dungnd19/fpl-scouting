package database

import (
	"database/sql"
	"fmt"
	"math"

	"fpl-bot/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

const timeDecayEpsilon = 0.2

// DB wraps the sql.DB connection
type DB struct {
	*sql.DB
}

// Config holds database configuration
type Config struct {
	Path string
}

// New creates a new database connection
func New(cfg Config) (*DB, error) {
	db, err := sql.Open("sqlite3", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	pragmas := []string{
		"PRAGMA cache_size=-2000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA busy_timeout=5000",
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// Repository provides database operations for the bot
type Repository struct {
	db *DB
}

// NewRepository creates a new repository
func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// GetPendingRecommendations retrieves pending recommendations
func (r *Repository) GetPendingRecommendations(limit int) ([]models.Recommendation, error) {
	rows, err := r.db.Query(`
		SELECT id, sell_player_id, buy_player_id, sell_player_name,
		       buy_player_name, reason, score, expected_points_gain, price_diff
		FROM recommendations
		WHERE status = 'pending'
		ORDER BY score DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recommendations: %w", err)
	}
	defer rows.Close()

	var recs []models.Recommendation
	for rows.Next() {
		var r models.Recommendation
		err := rows.Scan(&r.ID, &r.SellPlayerID, &r.BuyPlayerID,
			&r.SellPlayerName, &r.BuyPlayerName, &r.Reason,
			&r.Score, &r.ExpectedGain, &r.PriceDiff)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recommendation: %w", err)
		}
		recs = append(recs, r)
	}

	return recs, rows.Err()
}

func (r *Repository) GetUserChatID(userID string) (int64, error) {
	var chatID int64
	err := r.db.QueryRow("SELECT telegram_chat_id FROM user_state WHERE user_id = ?", userID).Scan(&chatID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get chat ID: %w", err)
	}
	return chatID, nil
}

func (r *Repository) RegisterChatID(userID string, chatID int64) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO user_state (user_id, telegram_chat_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, userID, chatID)
	if err != nil {
		return fmt.Errorf("failed to register chat ID: %w", err)
	}
	return nil
}

func (r *Repository) UpdateRecommendationStatus(recID int, status string) error {
	_, err := r.db.Exec("UPDATE recommendations SET status = ? WHERE id = ?", status, recID)
	if err != nil {
		return fmt.Errorf("failed to update recommendation status: %w", err)
	}
	return nil
}

func (r *Repository) GetRecommendation(recID int) (*models.Recommendation, error) {
	var rec models.Recommendation
	err := r.db.QueryRow(`
		SELECT id, sell_player_id, buy_player_id, sell_player_name, buy_player_name,
		       reason, score, expected_points_gain, price_diff
		FROM recommendations WHERE id = ?
	`, recID).Scan(&rec.ID, &rec.SellPlayerID, &rec.BuyPlayerID,
		&rec.SellPlayerName, &rec.BuyPlayerName, &rec.Reason,
		&rec.Score, &rec.ExpectedGain, &rec.PriceDiff)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("recommendation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get recommendation: %w", err)
	}
	return &rec, nil
}

func (r *Repository) GetPlayerPrice(playerID int) (int, error) {
	var price int
	err := r.db.QueryRow("SELECT now_cost FROM players WHERE id = ?", playerID).Scan(&price)
	if err != nil {
		return 0, fmt.Errorf("failed to get player price: %w", err)
	}
	return price, nil
}

func (r *Repository) GetSystemStatus() (map[string]interface{}, error) {
	status := make(map[string]interface{})

	var lastFetch, lastAnalyze string
	r.db.QueryRow("SELECT value FROM metadata WHERE key = 'last_fetch'").Scan(&lastFetch)
	r.db.QueryRow("SELECT value FROM metadata WHERE key = 'last_analyze'").Scan(&lastAnalyze)

	var playerCount, recCount int
	r.db.QueryRow("SELECT COUNT(*) FROM players").Scan(&playerCount)
	r.db.QueryRow("SELECT COUNT(*) FROM recommendations WHERE status = 'pending'").Scan(&recCount)

	status["last_fetch"] = lastFetch
	status["last_analyze"] = lastAnalyze
	status["player_count"] = playerCount
	status["pending_recommendations"] = recCount

	return status, nil
}

func (r *Repository) LogTransfer(userID string, recID int, request, response, status, errorMsg string) error {
	_, err := r.db.Exec(`
		INSERT INTO transfer_log (
			user_id, recommendation_id, request, response, status, error_message
		) VALUES (?, ?, ?, ?, ?, ?)
	`, userID, recID, request, response, status, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to log transfer: %w", err)
	}
	return nil
}

func (r *Repository) GetMyTeamPlayerIDs(userID string) (map[int]bool, error) {
	rows, err := r.db.Query("SELECT player_id FROM my_team WHERE user_id = ?", userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query my_team: %w", err)
	}
	defer rows.Close()

	ids := make(map[int]bool)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	return ids, rows.Err()
}

func (r *Repository) GetMyTeamSellingPrices(userID string) (map[int]int, error) {
	rows, err := r.db.Query("SELECT player_id, selling_price FROM my_team WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prices := make(map[int]int)
	for rows.Next() {
		var id, price int
		if err := rows.Scan(&id, &price); err != nil {
			return nil, err
		}
		prices[id] = price
	}
	return prices, rows.Err()
}

func (r *Repository) GetBank() (int, error) {
	val, err := r.GetMetadata("bank")
	if err != nil || val == "" {
		return 0, err
	}
	var bank int
	fmt.Sscanf(val, "%d", &bank)
	return bank, nil
}

type MyTeamPlayer struct {
	PlayerID      int
	WebName       string
	TeamName      string
	Position      int
	NowCost       int
	SellingPrice  int
	TotalPoints   int
	Form          float64
	IsCaptain     bool
	IsViceCaptain bool
	SquadPosition int
}

func (r *Repository) GetMyTeamFull(userID string) ([]MyTeamPlayer, error) {
	rows, err := r.db.Query(`
		SELECT
			m.player_id, p.web_name, COALESCE(t.short_name, ''), p.element_type,
			p.now_cost, COALESCE(NULLIF(m.selling_price, 0), p.now_cost),
			p.total_points, CAST(p.form AS REAL),
			m.is_captain, m.is_vice_captain, m.position
		FROM my_team m
		JOIN players p ON m.player_id = p.id
		LEFT JOIN teams t ON p.team = t.id
		WHERE m.user_id = ?
		ORDER BY m.position
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var players []MyTeamPlayer
	for rows.Next() {
		var p MyTeamPlayer
		err := rows.Scan(
			&p.PlayerID, &p.WebName, &p.TeamName, &p.Position,
			&p.NowCost, &p.SellingPrice, &p.TotalPoints, &p.Form,
			&p.IsCaptain, &p.IsViceCaptain, &p.SquadPosition,
		)
		if err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

func (r *Repository) GetAllPlayers() ([]models.PlayerScore, error) {
	rows, err := r.db.Query(`
		SELECT
			p.id, p.web_name, p.team, COALESCE(t.short_name, ''), p.element_type,
			p.now_cost, p.total_points, p.minutes, p.status,
			COALESCE(p.chance_of_playing_next_round, 100),
			CAST(p.form AS REAL), CAST(p.points_per_game AS REAL)
		FROM players p
		LEFT JOIN teams t ON p.team = t.id
		ORDER BY p.total_points DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query players: %w", err)
	}
	defer rows.Close()

	var players []models.PlayerScore
	for rows.Next() {
		var p models.PlayerScore
		var status string
		var avail int

		err := rows.Scan(
			&p.PlayerID, &p.WebName, &p.TeamID, &p.TeamName, &p.Position,
			&p.NowCost, &p.TotalPoints, &p.Minutes, &status, &avail,
			&p.Form, &p.PointsPerGame,
		)
		if err != nil {
			return nil, err
		}

		p.Status = status
		p.Availability = float64(avail) / 100.0
		if status == "i" || status == "s" {
			p.Availability = 0
		} else if status == "d" {
			p.Availability *= 0.5
		}

		players = append(players, p)
	}
	return players, rows.Err()
}

func (r *Repository) GetActivePlayersWithExpected(minMinutes int) ([]models.PlayerScore, error) {
	rows, err := r.db.Query(`
		SELECT
			p.id, p.web_name, p.team, COALESCE(t.short_name, ''), p.element_type,
			p.now_cost, p.total_points, p.minutes, p.status,
			COALESCE(p.chance_of_playing_next_round, 100),
			CAST(p.form AS REAL), CAST(p.points_per_game AS REAL)
		FROM players p
		LEFT JOIN teams t ON p.team = t.id
		WHERE p.minutes > ?
		ORDER BY p.total_points DESC
	`, minMinutes)
	if err != nil {
		return nil, fmt.Errorf("failed to query players: %w", err)
	}
	defer rows.Close()

	var players []models.PlayerScore
	for rows.Next() {
		var p models.PlayerScore
		var status string
		var avail int

		err := rows.Scan(
			&p.PlayerID, &p.WebName, &p.TeamID, &p.TeamName, &p.Position,
			&p.NowCost, &p.TotalPoints, &p.Minutes, &status, &avail,
			&p.Form, &p.PointsPerGame,
		)
		if err != nil {
			return nil, err
		}

		p.Status = status
		p.Availability = float64(avail) / 100.0
		if status == "i" || status == "s" {
			p.Availability = 0
		} else if status == "d" {
			p.Availability *= 0.5
		}

		players = append(players, p)
	}
	return players, rows.Err()
}

type PlayerHistoryStats struct {
	Games     int
	Minutes   int
	Points    int
	XG        float64
	XA        float64
	XGI       float64
	XGC       float64
	CS        int
	Goals     int
	Assists   int
	TeamGoals int
	CBIT      int
	Tackles   int
	Recoveries int
}

// PHASE 1: Weighted stats with exponential time decay.
// Uses exp(-epsilon * games_ago) where epsilon=0.2.
// A game 1 GW ago gets weight ~0.82, game 5 GW ago gets ~0.37.

// GetMaxEvent returns the maximum gameweek in player_history for the current season.
// Only considers events >= maxEvent - 10 to avoid counting previous season data.
func (r *Repository) GetMaxEvent() (int, error) {
	var maxEvent sql.NullInt64
	err := r.db.QueryRow("SELECT COALESCE(MAX(event), 0) FROM player_history").Scan(&maxEvent)
	if err != nil {
		return 0, err
	}
	if !maxEvent.Valid || maxEvent.Int64 == 0 {
		return 0, nil
	}
	return int(maxEvent.Int64), nil
}

func (r *Repository) GetPlayerWeightedRecentStats(playerID int, numGames int) (*PlayerHistoryStats, error) {
	maxEvent, err := r.GetMaxEvent()
	if err != nil {
		return nil, err
	}
	if maxEvent == 0 {
		return r.GetPlayerRecentStats(playerID, numGames)
	}

	rows, err := r.db.Query(`
		SELECT event, minutes, total_points,
			CAST(expected_goals AS REAL), CAST(expected_assists AS REAL),
			CAST(expected_goal_involvements AS REAL), CAST(expected_goals_conceded AS REAL),
			clean_sheets, goals_scored, assists,
			COALESCE(clearances_blocks_interceptions,0),
			COALESCE(tackles,0), COALESCE(recoveries,0)
		FROM player_history
		WHERE player_id = ? AND minutes > 0
		ORDER BY event DESC
		LIMIT ?
	`, playerID, numGames)
	if err != nil {
		return nil, fmt.Errorf("failed to query weighted stats: %w", err)
	}
	defer rows.Close()

	var s PlayerHistoryStats
	var totalWeight float64

	for rows.Next() {
		var event, minutes, points, cs, goals, assists int
		var xg, xa, xgi, xgc float64
		var cbit, tackles, recoveries int

		if err := rows.Scan(&event, &minutes, &points, &xg, &xa, &xgi, &xgc, &cs, &goals, &assists, &cbit, &tackles, &recoveries); err != nil {
			return nil, err
		}

		weight := math.Exp(-timeDecayEpsilon * float64(maxEvent-event))
		totalWeight += weight
		s.Games++
		s.Minutes += int(float64(minutes) * weight)
		s.Points += int(float64(points) * weight)
		s.XG += xg * weight
		s.XA += xa * weight
		s.XGI += xgi * weight
		s.XGC += xgc * weight
		s.CS += int(float64(cs) * weight)
		s.Goals += int(float64(goals) * weight)
		s.Assists += int(float64(assists) * weight)
		s.CBIT += int(float64(cbit) * weight)
		s.Tackles += int(float64(tackles) * weight)
		s.Recoveries += int(float64(recoveries) * weight)
	}

	return &s, rows.Err()
}

// GetPlayerRecentStats returns aggregated expected stats for a player over last N games (no weighting, for reports)
func (r *Repository) GetPlayerRecentStats(playerID int, numGames int) (*PlayerHistoryStats, error) {
	row := r.db.QueryRow(`
		SELECT
			COUNT(*), COALESCE(SUM(minutes),0), COALESCE(SUM(total_points),0),
			COALESCE(SUM(CAST(expected_goals AS REAL)),0),
			COALESCE(SUM(CAST(expected_assists AS REAL)),0),
			COALESCE(SUM(CAST(expected_goal_involvements AS REAL)),0),
			COALESCE(SUM(CAST(expected_goals_conceded AS REAL)),0),
			COALESCE(SUM(clean_sheets),0),
			COALESCE(SUM(goals_scored),0), COALESCE(SUM(assists),0),
			COALESCE(SUM(COALESCE(clearances_blocks_interceptions,0)),0),
			COALESCE(SUM(COALESCE(tackles,0)),0),
			COALESCE(SUM(COALESCE(recoveries,0)),0),
			0, 0, 0
		FROM (
			SELECT * FROM player_history
			WHERE player_id = ? AND minutes > 0
			ORDER BY event DESC
			LIMIT ?
		)
	`, playerID, numGames)

	var s PlayerHistoryStats
	err := row.Scan(&s.Games, &s.Minutes, &s.Points, &s.XG, &s.XA, &s.XGI, &s.XGC, &s.CS,
		&s.Goals, &s.Assists, &s.CBIT, &s.Tackles, &s.Recoveries, &s.TeamGoals)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) GetPlayerSeasonStats(playerID int) (*PlayerHistoryStats, error) {
	row := r.db.QueryRow(`
		SELECT
			COUNT(*), COALESCE(SUM(minutes),0), COALESCE(SUM(total_points),0),
			COALESCE(SUM(CAST(expected_goals AS REAL)),0),
			COALESCE(SUM(CAST(expected_assists AS REAL)),0),
			COALESCE(SUM(CAST(expected_goal_involvements AS REAL)),0),
			COALESCE(SUM(CAST(expected_goals_conceded AS REAL)),0),
			COALESCE(SUM(clean_sheets),0),
			COALESCE(SUM(goals_scored),0), COALESCE(SUM(assists),0),
			COALESCE(SUM(COALESCE(clearances_blocks_interceptions,0)),0),
			COALESCE(SUM(COALESCE(tackles,0)),0),
			COALESCE(SUM(COALESCE(recoveries,0)),0),
			0, 0, 0
		FROM player_history
		WHERE player_id = ? AND minutes > 0
	`, playerID)

	var s PlayerHistoryStats
	err := row.Scan(&s.Games, &s.Minutes, &s.Points, &s.XG, &s.XA, &s.XGI, &s.XGC, &s.CS,
		&s.Goals, &s.Assists, &s.CBIT, &s.Tackles, &s.Recoveries, &s.TeamGoals)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *Repository) StoreRecommendations(userID string, recs []models.Recommendation) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, rec := range recs {
		_, err := tx.Exec(`
			INSERT INTO recommendations (
				user_id, sell_player_id, buy_player_id,
				sell_player_name, buy_player_name,
				reason, score, expected_points_gain, price_diff, meta
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '{}')
		`, userID, rec.SellPlayerID, rec.BuyPlayerID,
			rec.SellPlayerName, rec.BuyPlayerName,
			rec.Reason, rec.Score, rec.ExpectedGain, rec.PriceDiff)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *Repository) GetMetadata(key string) (string, error) {
	var value string
	err := r.db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func (r *Repository) SetMetadata(key, value string) error {
	_, err := r.db.Exec(`
		INSERT OR REPLACE INTO metadata (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, key, value)
	return err
}

func (r *Repository) CleanOldRecommendations(daysToKeep int) error {
	_, err := r.db.Exec(`
		DELETE FROM recommendations
		WHERE timestamp < datetime('now', '-' || ? || ' days')
		AND status IN ('sent', 'rejected', 'executed', 'failed')
	`, daysToKeep)
	return err
}

func (db *DB) Close() error {
	return db.DB.Close()
}

// PHASE 2: Team ratings from stored team strength data

// GetTeamRatings builds Poisson-style ratings using stored team strength data.
func (r *Repository) GetTeamRatings() (map[int]models.TeamRating, error) {
	rows, err := r.db.Query(`
		SELECT id, short_name,
			CAST(strength_attack_home + strength_attack_away AS REAL) / 20.0,
			CAST(strength_defence_home + strength_defence_away AS REAL) / 20.0
		FROM teams
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ratings := make(map[int]models.TeamRating)
	for rows.Next() {
		var tr models.TeamRating
		var shortName string
		err := rows.Scan(&tr.TeamID, &shortName, &tr.AttackStrength, &tr.DefenceStrength)
		if err != nil {
			return nil, err
		}
		tr.TeamName = shortName
		tr.HomeAdvantage = 1.15
		ratings[tr.TeamID] = tr
	}
	return ratings, rows.Err()
}

// GetTeamFromPlayer returns the team ID for a player
func (r *Repository) GetTeamFromPlayer(playerID int) (int, error) {
	var teamID int
	err := r.db.QueryRow("SELECT team FROM players WHERE id = ?", playerID).Scan(&teamID)
	return teamID, err
}

// PHASE 4: Fixture lookahead for multi-week scoring

// GetNextFixture returns the next fixture for a given team and gameweek range
func (r *Repository) GetNextFixture(teamID int, gameweek int) (*models.FixtureInfo, error) {
	row := r.db.QueryRow(`
		SELECT id, event, team_h, team_a, team_h_difficulty, team_a_difficulty
		FROM fixtures
		WHERE (team_h = ? OR team_a = ?) AND event >= ? AND finished = 0
		ORDER BY event ASC
		LIMIT 1
	`, teamID, teamID, gameweek)

	var id, event, teamH, teamA, diffH, diffA int
	err := row.Scan(&id, &event, &teamH, &teamA, &diffH, &diffA)
	if err != nil {
		return nil, err
	}

	fi := &models.FixtureInfo{
		FixtureID: id,
		Gameweek:  event,
	}
	if teamH == teamID {
		fi.IsHome = true
		fi.OpponentTeamID = teamA
		fi.Difficulty = diffH
	} else {
		fi.IsHome = false
		fi.OpponentTeamID = teamH
		fi.Difficulty = diffA
	}
	return fi, nil
}

// GetUpcomingFixtures returns fixtures for a team over the next N gameweeks
func (r *Repository) GetUpcomingFixtures(teamID int, startGW int, numGWs int) ([]models.FixtureInfo, error) {
	rows, err := r.db.Query(`
		SELECT id, event, team_h, team_a, team_h_difficulty, team_a_difficulty
		FROM fixtures
		WHERE (team_h = ? OR team_a = ?) AND event >= ? AND event < ?
		ORDER BY event ASC
		LIMIT ?
	`, teamID, teamID, startGW, startGW+numGWs, numGWs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fixtures []models.FixtureInfo
	for rows.Next() {
		var id, event, teamH, teamA, diffH, diffA int
		if err := rows.Scan(&id, &event, &teamH, &teamA, &diffH, &diffA); err != nil {
			return nil, err
		}
		fi := models.FixtureInfo{
			FixtureID: id,
			Gameweek:  event,
		}
		if teamH == teamID {
			fi.IsHome = true
			fi.OpponentTeamID = teamA
			fi.Difficulty = diffH
		} else {
			fi.IsHome = false
			fi.OpponentTeamID = teamH
			fi.Difficulty = diffA
		}
		fixtures = append(fixtures, fi)
	}
	return fixtures, rows.Err()
}

// GetCurrentGameweek returns the current gameweek from metadata or fixtures
func (r *Repository) GetCurrentGameweek() (int, error) {
	val, err := r.GetMetadata("current_gw")
	if err == nil {
		var gw int
		fmt.Sscanf(val, "%d", &gw)
		if gw > 0 {
			return gw, nil
		}
	}
	var gw int
	err = r.db.QueryRow(`
		SELECT COALESCE(MIN(event), 1) FROM fixtures WHERE COALESCE(finished, 0) = 0
	`).Scan(&gw)
	return gw, err
}

// GetLastNGameweeks returns the last N completed gameweek numbers
func (r *Repository) GetLastNGameweeks(n int) ([]int, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT event FROM player_history
		WHERE event IS NOT NULL
		ORDER BY event DESC LIMIT ?
	`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var gws []int
	for rows.Next() {
		var gw int
		if err := rows.Scan(&gw); err != nil {
			return nil, err
		}
		gws = append(gws, gw)
	}
	return gws, rows.Err()
}

// GetPlayerHistoryForGW returns a player's history entry for a specific gameweek
func (r *Repository) GetPlayerHistoryForGW(playerID int, gw int) (*PlayerHistoryStats, error) {
	row := r.db.QueryRow(`
		SELECT
			1, COALESCE(minutes,0), COALESCE(total_points,0),
			COALESCE(CAST(expected_goals AS REAL),0),
			COALESCE(CAST(expected_assists AS REAL),0),
			COALESCE(CAST(expected_goal_involvements AS REAL),0),
			COALESCE(CAST(expected_goals_conceded AS REAL),0),
			COALESCE(clean_sheets,0),
			COALESCE(goals_scored,0), COALESCE(assists,0),
			COALESCE(COALESCE(clearances_blocks_interceptions,0),0),
			COALESCE(COALESCE(tackles,0),0),
			COALESCE(COALESCE(recoveries,0),0),
			0, 0, 0
		FROM player_history
		WHERE player_id = ? AND event = ?
	`, playerID, gw)

	var s PlayerHistoryStats
	err := row.Scan(&s.Games, &s.Minutes, &s.Points, &s.XG, &s.XA, &s.XGI, &s.XGC, &s.CS,
		&s.Goals, &s.Assists, &s.CBIT, &s.Tackles, &s.Recoveries, &s.TeamGoals)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// PHASE 5: Get all players with goal data for empirical Bayes prior computation
func (r *Repository) GetPositionStats(lastNGameweeks int) ([]PositionStatSummary, error) {
	gws, err := r.GetLastNGameweeks(lastNGameweeks)
	if err != nil {
		return nil, err
	}
	if len(gws) == 0 {
		return nil, nil
	}

	rows, err := r.db.Query(`
		SELECT
			p.element_type,
			SUM(h.goals_scored) AS total_goals,
			SUM(h.assists) AS total_assists,
			SUM(h.minutes) AS total_minutes,
			COUNT(DISTINCT h.player_id) AS num_players
		FROM player_history h
		JOIN players p ON h.player_id = p.id
		WHERE h.minutes > 0
		GROUP BY p.element_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []PositionStatSummary
	for rows.Next() {
		var s PositionStatSummary
		err := rows.Scan(&s.Position, &s.TotalGoals, &s.TotalAssists, &s.TotalMinutes, &s.NumPlayers)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}
	return summaries, rows.Err()
}

type PositionStatSummary struct {
	Position     int
	TotalGoals   int
	TotalAssists int
	TotalMinutes int
	NumPlayers   int
}

// PHASE 6: Free transfer tracking

// GetFreeTransfers returns the user's current free transfers count
func (r *Repository) GetFreeTransfers() (int, error) {
	val, err := r.GetMetadata("free_transfers")
	if err != nil || val == "" {
		return 1, nil // default to 1 free transfer
	}
	var ft int
	fmt.Sscanf(val, "%d", &ft)
	return ft, nil
}

// GetNextGameweekForTransfers returns the gameweek to optimize transfers for
func (r *Repository) GetNextGameweekForTransfers() (int, error) {
	return r.GetCurrentGameweek()
}

// ComputeSellPrice uses FPL sell-price rules: (purchase + current) / 2 if profit, else current
func ComputeSellPrice(purchasePrice, currentPrice int) int {
	sellPrice := currentPrice
	if currentPrice > purchasePrice {
		sellPrice = (purchasePrice + currentPrice) / 2
	}
	return sellPrice
}

// RoundFloat rounds a float to n decimal places
func RoundFloat(val float64, precision int) float64 {
	ratio := math.Pow(10, float64(precision))
	return math.Round(val*ratio) / ratio
}

// COLD-START: Multi-season blended stats using vaastav historical data.

// GetPlayerMultiSeasonBlendedStats returns time-decayed stats blending current season
// (player_history) with prior seasons (season_history via cross_season_map).
//
// Uses only the N most recent seasons (default 2: 2025-26 weight 1.0, 2024-25 weight 0.61).
// Older seasons (2023-24, 2022-23) are ignored as too stale.
func (r *Repository) GetPlayerMultiSeasonBlendedStats(playerID int, numRecentGames int) (*PlayerHistoryStats, error) {
	return r.blendStatsN(playerID, numRecentGames, 2, 1000)
}

// GetPlayerInitSquadStats blends current stats with prior seasons for initial-squad building.
// isNewSeason controls the blend strategy:
//   - New season: priorRatio phases out linearly over 20 games (100% prior → 0% prior)
//     This approximates "last 20 games regardless of season" when current data is sparse.
//   - Mid-season: fixed 50/50 blend with 2 prior seasons as stable baseline.
func (r *Repository) GetPlayerInitSquadStats(playerID int, isNewSeason bool) (*PlayerHistoryStats, error) {
	currentStats, _ := r.GetPlayerWeightedRecentStats(playerID, 20)
	priorNames := r.getCrossSeasonNames(playerID)

	if len(priorNames) == 0 {
		return currentStats, nil
	}

	priorStats, err := r.getPriorSeasonStatsN(priorNames, 2)
	if err != nil || priorStats == nil || priorStats.Games == 0 || priorStats.Minutes < 1000 {
		return currentStats, nil
	}

	currentGames := 0
	if currentStats != nil {
		currentGames = currentStats.Games
	}

	var priorRatio float64
	if isNewSeason {
		priorRatio = 1.0 - float64(currentGames)/20.0
		if priorRatio < 0 {
			priorRatio = 0
		}
	} else {
		priorRatio = 0.5
	}
	currentRatio := 1.0 - priorRatio

	if currentStats == nil {
		return priorStats, nil
	}

	blended := &PlayerHistoryStats{
		Games:     int(float64(currentStats.Games)*currentRatio + float64(priorStats.Games)*priorRatio),
		Minutes:   int(float64(currentStats.Minutes)*currentRatio + float64(priorStats.Minutes)*priorRatio),
		Points:    int(float64(currentStats.Points)*currentRatio + float64(priorStats.Points)*priorRatio),
		XG:        currentStats.XG*currentRatio + priorStats.XG*priorRatio,
		XA:        currentStats.XA*currentRatio + priorStats.XA*priorRatio,
		XGI:       currentStats.XGI*currentRatio + priorStats.XGI*priorRatio,
		XGC:       currentStats.XGC*currentRatio + priorStats.XGC*priorRatio,
		CS:        int(float64(currentStats.CS)*currentRatio + float64(priorStats.CS)*priorRatio),
		Goals:     int(float64(currentStats.Goals)*currentRatio + float64(priorStats.Goals)*priorRatio),
		Assists:   int(float64(currentStats.Assists)*currentRatio + float64(priorStats.Assists)*priorRatio),
		TeamGoals: currentStats.TeamGoals,
	}

	return blended, nil
}

// blendStatsN blends using only the N most recent prior seasons, requiring minMins total.
func (r *Repository) blendStatsN(playerID int, numRecentGames int, maxPriorSeasons int, minMinutes int) (*PlayerHistoryStats, error) {
	currentStats, _ := r.GetPlayerWeightedRecentStats(playerID, numRecentGames)

	priorNames := r.getCrossSeasonNames(playerID)
	if len(priorNames) == 0 {
		return currentStats, nil
	}

	priorStats, err := r.getPriorSeasonStatsN(priorNames, maxPriorSeasons)
	if err != nil || priorStats == nil || priorStats.Games == 0 {
		return currentStats, nil
	}

	// Require minimum total minutes to filter sub-only players
	if priorStats.Minutes < minMinutes {
		return nil, nil
	}

	currentGames := 0
	if currentStats != nil {
		currentGames = currentStats.Games
	}
	currentRatio := float64(currentGames) / 3.0
	if currentRatio > 1.0 {
		currentRatio = 1.0
	}
	priorRatio := 1.0 - currentRatio

	if currentStats == nil {
		currentStats = &PlayerHistoryStats{}
	}

	blended := &PlayerHistoryStats{
		Games:   currentStats.Games + int(float64(priorStats.Games)*priorRatio),
		Minutes: currentStats.Minutes + int(float64(priorStats.Minutes)*priorRatio),
		Points:  currentStats.Points + int(float64(priorStats.Points)*priorRatio),
		XG:      currentStats.XG + priorStats.XG*priorRatio,
		XA:      currentStats.XA + priorStats.XA*priorRatio,
		XGI:     currentStats.XGI + priorStats.XGI*priorRatio,
		XGC:     currentStats.XGC + priorStats.XGC*priorRatio,
		CS:      currentStats.CS + int(float64(priorStats.CS)*priorRatio),
		Goals:   currentStats.Goals + int(float64(priorStats.Goals)*priorRatio),
		Assists: currentStats.Assists + int(float64(priorStats.Assists)*priorRatio),
		TeamGoals: currentStats.TeamGoals,
	}

	return blended, nil
}

// getCrossSeasonNames returns the historical player names from cross_season_map for a playerID.
func (r *Repository) getCrossSeasonNames(playerID int) map[string]float64 {
	rows, err := r.db.Query(`
		SELECT prior_player_name, COALESCE(confidence, 1.0)
		FROM cross_season_map
		WHERE current_player_id = ?
	`, playerID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	names := make(map[string]float64)
	for rows.Next() {
		var name string
		var conf float64
		if err := rows.Scan(&name, &conf); err != nil {
			continue
		}
		names[name] = conf
	}
	return names
}

// getPriorSeasonStats aggregates stats from season_history for given player names,
// using only the N most recent seasons with decay weighting.
func (r *Repository) getPriorSeasonStatsN(names map[string]float64, maxSeasons int) (*PlayerHistoryStats, error) {
	seasons, err := r.getSeasonOrder()
	if err != nil || len(seasons) == 0 {
		return nil, err
	}

	// Take only the N most recent seasons
	if len(seasons) > maxSeasons {
		seasons = seasons[:maxSeasons]
	}

	var totalWeight float64
	var s PlayerHistoryStats

	for priorName := range names {
		for seasonIdx, season := range seasons {
			seasonStats, err := r.querySeasonStatsForSeason(priorName, season)
			if err != nil || seasonStats == nil || seasonStats.Games == 0 {
				continue
			}
			// Weight: exp(-0.5 * seasonIndex). Season 0 (most recent) = 1.0, Season 1 = 0.61
			w := math.Exp(-0.5 * float64(seasonIdx))
			totalWeight += w

			s.Games += int(float64(seasonStats.Games) * w)
			s.Minutes += int(float64(seasonStats.Minutes) * w)
			s.Points += int(float64(seasonStats.Points) * w)
			s.XG += seasonStats.XG * w
			s.XA += seasonStats.XA * w
			s.XGI += seasonStats.XGI * w
			s.XGC += seasonStats.XGC * w
			s.CS += int(float64(seasonStats.CS) * w)
			s.Goals += int(float64(seasonStats.Goals) * w)
			s.Assists += int(float64(seasonStats.Assists) * w)
		}
	}

	if totalWeight > 0 {
		s.Games = int(float64(s.Games) / totalWeight)
		s.Minutes = int(float64(s.Minutes) / totalWeight)
		s.Points = int(float64(s.Points) / totalWeight)
		s.XG /= totalWeight
		s.XA /= totalWeight
		s.XGI /= totalWeight
		s.XGC /= totalWeight
		s.CS = int(float64(s.CS) / totalWeight)
		s.Goals = int(float64(s.Goals) / totalWeight)
		s.Assists = int(float64(s.Assists) / totalWeight)
	}

	return &s, nil
}

func (r *Repository) getSeasonOrder() ([]string, error) {
	rows, err := r.db.Query(`
		SELECT DISTINCT season FROM season_history ORDER BY season DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var seasons []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		seasons = append(seasons, s)
	}
	return seasons, nil
}

func seasonsToNameList(names map[string]float64) []string {
	var list []string
	for name := range names {
		list = append(list, name)
	}
	return list
}

// querySeasonStatsForSeason gets aggregated stats for a player name from a specific season.
// Skips players with fewer than minGames appearances to avoid overfitting on tiny samples.
func (r *Repository) querySeasonStatsForSeason(priorName string, season string) (*seasonStatSimple, error) {
	const minGames = 10
	row := r.db.QueryRow(`
		SELECT
			COALESCE(SUM(minutes),0), COALESCE(SUM(total_points),0),
			COALESCE(SUM(CAST(expected_goals AS REAL)),0),
			COALESCE(SUM(CAST(expected_assists AS REAL)),0),
			COALESCE(SUM(CAST(expected_goal_involvements AS REAL)),0),
			COALESCE(SUM(CAST(expected_goals_conceded AS REAL)),0),
			COALESCE(SUM(clean_sheets),0),
			COALESCE(SUM(goals_scored),0), COALESCE(SUM(assists),0),
			COUNT(*)
		FROM season_history
		WHERE LOWER(player_name) = LOWER(?) AND season = ? AND minutes > 0
		HAVING COUNT(*) >= ?
	`, priorName, season, minGames)

	var s seasonStatSimple
	err := row.Scan(&s.Minutes, &s.Points, &s.XG, &s.XA, &s.XGI, &s.XGC,
		&s.CS, &s.Goals, &s.Assists, &s.Games)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

type seasonStatSimple struct {
	Games   int
	Minutes int
	Points  int
	Goals   int
	Assists int
	CS      int
	XG      float64
	XA      float64
	XGI     float64
	XGC     float64
}

// IsColdStart returns true if the current season has fewer than minGames of data.
// Uses a heuristic: only counts events in the most recent contiguous block of gameweeks.
func (r *Repository) IsColdStart(minGames int) (bool, error) {
	// Check if there are separate seasons in the data by looking for a gap > 5 between events
	var maxEvent int
	r.db.QueryRow("SELECT COALESCE(MAX(event), 0) FROM player_history").Scan(&maxEvent)
	if maxEvent == 0 {
		return true, nil
	}

	// Count events in the "current season" — events >= maxEvent - 5 (handles end-of-season)
	var gameCount int
	err := r.db.QueryRow(`
		SELECT COUNT(DISTINCT event) FROM player_history
		WHERE minutes > 0 AND event > ?
	`, maxEvent-10).Scan(&gameCount)
	if err != nil {
		return true, err
	}
	return gameCount < minGames, nil
}

// HasSeasonData checks if season_history table has been populated from vaastav.
func (r *Repository) HasSeasonData() (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM season_history").Scan(&count)
	return count > 0, err
}
