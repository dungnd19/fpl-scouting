package models

import "time"

// Event represents a gameweek in FPL
type Event struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Finished  bool   `json:"finished"`
	IsCurrent bool   `json:"is_current"`
	IsNext    bool   `json:"is_next"`
}

// Team represents a Premier League team
type Team struct {
	ID                  int    `json:"id"`
	Code                int    `json:"code"`
	Name                string `json:"name"`
	ShortName           string `json:"short_name"`
	Strength            int    `json:"strength"`
	StrengthOverallHome int    `json:"strength_overall_home"`
	StrengthOverallAway int    `json:"strength_overall_away"`
	StrengthAttackHome  int    `json:"strength_attack_home"`
	StrengthAttackAway  int    `json:"strength_attack_away"`
	StrengthDefenceHome int    `json:"strength_defence_home"`
	StrengthDefenceAway int    `json:"strength_defence_away"`
}

// Player represents an FPL player
type Player struct {
	ID                       int     `json:"id"`
	Code                     int     `json:"code"`
	FirstName                string  `json:"first_name"`
	SecondName               string  `json:"second_name"`
	WebName                  string  `json:"web_name"`
	Team                     int     `json:"team"`
	ElementType              int     `json:"element_type"` // 1=GK, 2=DEF, 3=MID, 4=FWD
	NowCost                  int     `json:"now_cost"`     // price in 0.1m units
	Form                     string  `json:"form"`
	PointsPerGame            string  `json:"points_per_game"`
	SelectedByPercent        string  `json:"selected_by_percent"`
	TotalPoints              int     `json:"total_points"`
	Minutes                  int     `json:"minutes"`
	GoalsScored              int     `json:"goals_scored"`
	Assists                  int     `json:"assists"`
	CleanSheets              int     `json:"clean_sheets"`
	GoalsConceded            int     `json:"goals_conceded"`
	Saves                    int     `json:"saves"`
	Bonus                    int     `json:"bonus"`
	BPS                      int     `json:"bps"` // bonus points system
	Influence                string  `json:"influence"`
	Creativity               string  `json:"creativity"`
	Threat                   string  `json:"threat"`
	ICTIndex                 string  `json:"ict_index"`
	ExpectedGoals            string  `json:"expected_goals"`
	ExpectedAssists          string  `json:"expected_assists"`
	ExpectedGoalInvolvements string  `json:"expected_goal_involvements"`
	ExpectedGoalsConceded    string  `json:"expected_goals_conceded"`
	Status                   string  `json:"status"` // a=available, d=doubtful, i=injured, s=suspended
	ChanceOfPlayingNextRound *int    `json:"chance_of_playing_next_round"`
}

// BootstrapData represents the response from bootstrap-static API
type BootstrapData struct {
	Events   []Event  `json:"events"`
	Teams    []Team   `json:"teams"`
	Elements []Player `json:"elements"`
}

// HistoryEntry represents a player's performance in a single gameweek
type HistoryEntry struct {
	Element                  int    `json:"element"`
	Fixture                  int    `json:"fixture"`
	OpponentTeam             int    `json:"opponent_team"`
	TotalPoints              int    `json:"total_points"`
	WasHome                  bool   `json:"was_home"`
	KickoffTime              string `json:"kickoff_time"`
	Round                    int    `json:"round"`
	Minutes                  int    `json:"minutes"`
	GoalsScored              int    `json:"goals_scored"`
	Assists                  int    `json:"assists"`
	CleanSheets              int    `json:"clean_sheets"`
	GoalsConceded            int    `json:"goals_conceded"`
	Saves                    int    `json:"saves"`
	Bonus                    int    `json:"bonus"`
	BPS                      int    `json:"bps"`
	Influence                string `json:"influence"`
	Creativity               string `json:"creativity"`
	Threat                   string `json:"threat"`
	ICTIndex                 string `json:"ict_index"`
	Value                    int    `json:"value"`
	Selected                 int    `json:"selected"`
	TransfersBalance         int    `json:"transfers_balance"`
	TransfersIn              int    `json:"transfers_in"`
	TransfersOut             int    `json:"transfers_out"`
	ExpectedGoals                 string `json:"expected_goals"`
	ExpectedAssists               string `json:"expected_assists"`
	ExpectedGoalInvolvements      string `json:"expected_goal_involvements"`
	ExpectedGoalsConceded         string `json:"expected_goals_conceded"`
	ClearancesBlocksInterceptions int    `json:"clearances_blocks_interceptions"`
	Tackles                       int    `json:"tackles"`
	Recoveries                    int    `json:"recoveries"`
}

// ElementSummary represents the response from element-summary API
type ElementSummary struct {
	History []HistoryEntry `json:"history"`
}

// PlayerScore represents a scored player for analysis
type PlayerScore struct {
	PlayerID       int
	WebName        string
	Team           int
	Position       int
	NowCost        int
	TotalPoints    int
	Form           float64
	PointsPerGame  float64
	ExpectedPoints float64
	Value          float64 // points per cost
	RecentForm     float64 // last 5 games
	Availability   float64 // injury/suspension risk
	OverallScore   float64
}

// TransferRecommendation represents a suggested transfer
type TransferRecommendation struct {
	SellPlayerID   int
	BuyPlayerID    int
	SellPlayerName string
	BuyPlayerName  string
	ExpectedGain   float64
	PriceDiff      float64
	Reason         string
	Score          float64
	Timestamp      time.Time
}

// MyTeam represents the user's current FPL team
type MyTeam struct {
	Picks []TeamPick `json:"picks"`
	Chips []Chip     `json:"chips"`
	Transfers struct {
		Cost  int `json:"cost"`
		Status string `json:"status"`
		Limit int `json:"limit"`
		Made  int `json:"made"`
		Bank  int `json:"bank"`
		Value int `json:"value"`
	} `json:"transfers"`
}

// TeamPick represents a player in the user's team
type TeamPick struct {
	Element        int  `json:"element"`         // Player ID
	Position       int  `json:"position"`        // Position in squad (1-15)
	IsCaptain      bool `json:"is_captain"`
	IsViceCaptain  bool `json:"is_vice_captain"`
	Multiplier     int  `json:"multiplier"`
	SellingPrice   int  `json:"selling_price"`   // Current selling price
	PurchasePrice  int  `json:"purchase_price"`  // Original purchase price
}

// Chip represents an available chip
type Chip struct {
	StatusForEntry string `json:"status_for_entry"`
	PlayedByEntry  []int  `json:"played_by_entry"`
	Name           string `json:"name"`
	Number         int    `json:"number"`
	StartEvent     int    `json:"start_event"`
	StopEvent      int    `json:"stop_event"`
	ChipType       string `json:"chip_type"`
}

// EntryInfo represents the public entry endpoint response
type EntryInfo struct {
	ID           int `json:"id"`
	CurrentEvent int `json:"current_event"`
}

// PublicPicks represents the public picks endpoint response
type PublicPicks struct {
	EntryHistory struct {
		Bank  int `json:"bank"`
		Value int `json:"value"`
	} `json:"entry_history"`
	Picks []PublicPick `json:"picks"`
}

// PublicPick represents a player pick from the public endpoint
type PublicPick struct {
	Element       int  `json:"element"`
	Position      int  `json:"position"`
	Multiplier    int  `json:"multiplier"`
	IsCaptain     bool `json:"is_captain"`
	IsViceCaptain bool `json:"is_vice_captain"`
}

// SeasonHistoryEntry represents a player-gameweek record from a historical season (vaastav data).
type SeasonHistoryEntry struct {
	Season                   string
	PlayerName               string
	Position                 string // "GK", "DEF", "MID", "FWD"
	TeamName                 string
	Event                    int
	Minutes                  int
	TotalPoints              int
	GoalsScored              int
	Assists                  int
	CleanSheets              int
	GoalsConceded            int
	ExpectedGoals            float64
	ExpectedAssists          float64
	ExpectedGoalInvolvements float64
	ExpectedGoalsConceded    float64
	Bonus                    int
	BPS                      int
	Influence                float64
	Creativity               float64
	Threat                   float64
	ICTIndex                 float64
	WasHome                  int
	OpponentTeam             string
	KickoffTime              string
	Value                    int
	Selected                 int
}
