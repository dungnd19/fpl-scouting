package models

import "time"

// Recommendation represents a transfer recommendation from the database
type Recommendation struct {
	ID             int
	SellPlayerID   int
	BuyPlayerID    int
	SellPlayerName string
	BuyPlayerName  string
	Reason         string
	Score          float64
	ExpectedGain   float64
	PriceDiff      float64
	Status         string
	Timestamp      time.Time
}

// TransferRequest represents a request to the FPL API
type TransferRequest struct {
	Transfers []Transfer `json:"transfers"`
}

// Transfer represents a single transfer
type Transfer struct {
	ElementIn     int `json:"element_in"`
	ElementOut    int `json:"element_out"`
	PurchasePrice int `json:"purchase_price"`
	SellingPrice  int `json:"selling_price"`
}

// TransferResult represents the result of a transfer execution
type TransferResult struct {
	Success  bool
	Error    string
	Response string
}

// UserState represents user state in the database
type UserState struct {
	UserID         string
	TelegramChatID int64
	FPLTeamID      string
	SessionCookie  string
}

// PlayerScore represents a scored player for analysis
type PlayerScore struct {
	PlayerID     int
	WebName      string
	TeamID       int
	TeamName     string
	Position     int // 1=GK, 2=DEF, 3=MID, 4=FWD
	NowCost      int // price in 0.1m units
	TotalPoints  int
	Minutes      int
	Status       string
	Availability float64

	Form          float64
	PointsPerGame float64

	// Per-90 metrics (computed from player_history with time-decay)
	XGPer90  float64
	XAPer90  float64
	XGIPer90 float64
	XGCPer90 float64
	CSRate   float64
	PPG      float64
	Games    int

	// DEFCON per-90 rates
	CBITPer90  float64
	CBIRTPer90 float64

	// Phase 1: time-weighted versions
	WeightedXGPer90  float64
	WeightedXAPer90  float64
	WeightedXGIPer90 float64
	WeightedXGCPer90 float64
	WeightedCSRate   float64
	WeightedPPG      float64

	// Phase 2: fixture-adjusted metrics
	FixtureAdjustedXScore float64
	FixtureDifficulty     float64

	// Phase 3: true FPL expected points
	ExpectedPoints        float64 // per-gameweek expected FPL points
	ExpectedPointsMultiGW float64 // discounted multi-GW expected points

	// Composite scores
	XScore       float64
	Value        float64
	OverallScore float64

	// Bayesian model (Phase 5)
	ProbScore        float64 // P(player scores | team scores)
	ProbAssist       float64 // P(player assists | team scores)
	PriorAlphaScore  float64 // Dirichlet alpha for goals
	PriorAlphaAssist float64 // Dirichlet alpha for assists
	PriorAlphaNeither float64
}

// PlayerReport represents a player entry in position reports
type PlayerReport struct {
	PlayerID  int
	WebName   string
	TeamName  string
	Position  int
	NowCost   int
	XScore    float64
	PPG       float64
	XGPer90   float64
	XAPer90   float64
	XGIPer90  float64
	XGCPer90  float64
	CSRate    float64
	Games     int
	Minutes   int

	ExpectedPoints float64
	Value          float64
}

// TeamRating holds Poisson-derived team strength ratings (Phase 2)
type TeamRating struct {
	TeamID          int
	TeamName        string
	AttackStrength  float64 // goals scored relative to league average (home+away)
	DefenceStrength float64 // goals conceded relative to league average
	HomeAdvantage   float64 // multiplier for home games
}

// FixtureInfo holds a player's upcoming fixture (Phase 2, 4)
type FixtureInfo struct {
	FixtureID      int
	Gameweek       int
	IsHome         bool
	OpponentTeamID int
	Difficulty     int // FPL fixture difficulty rating (1-5)
}

// TransferPlan represents a multi-GW transfer strategy (Phase 6)
type TransferPlan struct {
	TransfersOut []int
	TransfersIn  []int
	FreeTransfers int
	PointsHit     int // 4 * max(0, numTransfers - freeTransfers)
	ChipPlayed    string
	Gameweek      int
}

// Position constants
const (
	PosGK  = 1
	PosDEF = 2
	PosMID = 3
	PosFWD = 4
)

// PositionToName maps position code to name
var PositionNames = map[int]string{1: "GK", 2: "DEF", 3: "MID", 4: "FWD"}

// PositionToNameStr maps position code to display name
var PositionToNameStr = map[int]string{1: "Goalkeeper", 2: "Defender", 3: "Midfielder", 4: "Forward"}
