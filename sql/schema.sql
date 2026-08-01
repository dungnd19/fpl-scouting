-- FPL Scouting SQLite Schema
-- Optimized for low memory usage

-- Players table (from bootstrap-static)
CREATE TABLE IF NOT EXISTS players (
    id INTEGER PRIMARY KEY,
    code INTEGER,
    first_name TEXT,
    second_name TEXT,
    web_name TEXT,
    team INTEGER,
    element_type INTEGER, -- 1=GK, 2=DEF, 3=MID, 4=FWD
    now_cost INTEGER, -- price in 0.1m units
    form REAL,
    points_per_game REAL,
    selected_by_percent REAL,
    total_points INTEGER,
    minutes INTEGER,
    goals_scored INTEGER,
    assists INTEGER,
    clean_sheets INTEGER,
    goals_conceded INTEGER,
    saves INTEGER,
    bonus INTEGER,
    bps INTEGER, -- bonus points system
    influence REAL,
    creativity REAL,
    threat REAL,
    ict_index REAL,
    expected_goals REAL,
    expected_assists REAL,
    expected_goal_involvements REAL,
    expected_goals_conceded REAL,
    status TEXT, -- a=available, d=doubtful, i=injured, s=suspended
    chance_of_playing_next_round INTEGER,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_players_team ON players(team);
CREATE INDEX IF NOT EXISTS idx_players_element_type ON players(element_type);
CREATE INDEX IF NOT EXISTS idx_players_now_cost ON players(now_cost);

-- Player historical performance (from element-summary)
CREATE TABLE IF NOT EXISTS player_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    player_id INTEGER,
    event INTEGER, -- gameweek number
    fixture INTEGER,
    opponent_team INTEGER,
    total_points INTEGER,
    was_home INTEGER,
    kickoff_time TEXT,
    minutes INTEGER,
    goals_scored INTEGER,
    assists INTEGER,
    clean_sheets INTEGER,
    goals_conceded INTEGER,
    saves INTEGER,
    bonus INTEGER,
    bps INTEGER,
    influence REAL,
    creativity REAL,
    threat REAL,
    ict_index REAL,
    value INTEGER, -- price at that gameweek
    selected INTEGER, -- number of managers who selected
    transfers_balance INTEGER,
    transfers_in INTEGER,
    transfers_out INTEGER,
    expected_goals REAL,
    expected_assists REAL,
    expected_goal_involvements REAL,
    expected_goals_conceded REAL,
    clearances_blocks_interceptions INTEGER,
    tackles INTEGER,
    recoveries INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(player_id, event) -- prevent duplicates
);

CREATE INDEX IF NOT EXISTS idx_player_history_player ON player_history(player_id);
CREATE INDEX IF NOT EXISTS idx_player_history_event ON player_history(event);

-- Fixtures
CREATE TABLE IF NOT EXISTS fixtures (
    id INTEGER PRIMARY KEY,
    code INTEGER,
    event INTEGER, -- gameweek
    finished INTEGER,
    kickoff_time TEXT,
    started INTEGER,
    team_h INTEGER, -- home team
    team_a INTEGER, -- away team
    team_h_difficulty INTEGER,
    team_a_difficulty INTEGER,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(id)
);

CREATE INDEX IF NOT EXISTS idx_fixtures_event ON fixtures(event);
CREATE INDEX IF NOT EXISTS idx_fixtures_team_h ON fixtures(team_h);
CREATE INDEX IF NOT EXISTS idx_fixtures_team_a ON fixtures(team_a);

-- Teams (from bootstrap-static)
CREATE TABLE IF NOT EXISTS teams (
    id INTEGER PRIMARY KEY,
    code INTEGER,
    name TEXT,
    short_name TEXT,
    strength INTEGER,
    strength_overall_home INTEGER,
    strength_overall_away INTEGER,
    strength_attack_home INTEGER,
    strength_attack_away INTEGER,
    strength_defence_home INTEGER,
    strength_defence_away INTEGER,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Transfer recommendations
CREATE TABLE IF NOT EXISTS recommendations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT,
    sell_player_id INTEGER,
    buy_player_id INTEGER,
    sell_player_name TEXT,
    buy_player_name TEXT,
    reason TEXT,
    score REAL,
    expected_points_gain REAL,
    price_diff REAL,
    status TEXT DEFAULT 'pending', -- pending, sent, confirmed, executed
    meta TEXT -- JSON metadata
);

CREATE INDEX IF NOT EXISTS idx_recommendations_user ON recommendations(user_id);
CREATE INDEX IF NOT EXISTS idx_recommendations_status ON recommendations(status);
CREATE INDEX IF NOT EXISTS idx_recommendations_timestamp ON recommendations(timestamp);

-- Transfer execution log
CREATE TABLE IF NOT EXISTS transfer_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    user_id TEXT,
    recommendation_id INTEGER,
    request TEXT, -- JSON of transfer request
    response TEXT, -- JSON of API response
    status TEXT, -- success, failed, error
    error_message TEXT,
    FOREIGN KEY (recommendation_id) REFERENCES recommendations(id)
);

CREATE INDEX IF NOT EXISTS idx_transfer_log_user ON transfer_log(user_id);
CREATE INDEX IF NOT EXISTS idx_transfer_log_timestamp ON transfer_log(timestamp);

-- User state (for telegram bot)
CREATE TABLE IF NOT EXISTS user_state (
    user_id TEXT PRIMARY KEY,
    telegram_chat_id INTEGER,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Metadata table (for tracking fetch state, gameweek, etc.)
CREATE TABLE IF NOT EXISTS metadata (
    key TEXT PRIMARY KEY,
    value TEXT,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- My Team table (stores user's current FPL team)
CREATE TABLE IF NOT EXISTS my_team (
    user_id TEXT,
    player_id INTEGER,
    position INTEGER, -- 1-15 (squad position)
    is_captain INTEGER DEFAULT 0,
    is_vice_captain INTEGER DEFAULT 0,
    multiplier INTEGER DEFAULT 1,
    selling_price INTEGER, -- Current selling price
    purchase_price INTEGER, -- Original purchase price
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, player_id),
    FOREIGN KEY (player_id) REFERENCES players(id)
);

CREATE INDEX IF NOT EXISTS idx_my_team_user ON my_team(user_id);
CREATE INDEX IF NOT EXISTS idx_my_team_player ON my_team(player_id);

-- Season history (from vaastav/Fantasy-Premier-League historical data)
-- Stores per-player per-GW stats from past seasons for cold-start predictions
CREATE TABLE IF NOT EXISTS season_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    season TEXT NOT NULL, -- e.g. "2023-24", "2022-23", "2024-25"
    player_name TEXT NOT NULL, -- full name (used for cross-season mapping)
    team_name TEXT, -- team short name
    position INTEGER, -- 1=GK, 2=DEF, 3=MID, 4=FWD
    event INTEGER, -- gameweek number
    minutes INTEGER,
    total_points INTEGER,
    goals_scored INTEGER,
    assists INTEGER,
    clean_sheets INTEGER,
    goals_conceded INTEGER,
    expected_goals REAL,
    expected_assists REAL,
    expected_goal_involvements REAL,
    expected_goals_conceded REAL,
    bonus INTEGER,
    bps INTEGER,
    influence REAL,
    creativity REAL,
    threat REAL,
    ict_index REAL,
    was_home INTEGER,
    opponent_team TEXT,
    kickoff_time TEXT,
    value INTEGER, -- price at that GW
    selected INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_season_history_name ON season_history(player_name, season);
CREATE INDEX IF NOT EXISTS idx_season_history_season ON season_history(season);

-- Cross-season player name mapping
-- Maps current-season player IDs to prior-season player names
CREATE TABLE IF NOT EXISTS cross_season_map (
    current_player_id INTEGER,
    season TEXT,
    prior_player_name TEXT,
    confidence REAL DEFAULT 1.0, -- match confidence (1.0 = exact name match)
    PRIMARY KEY (current_player_id, season),
    FOREIGN KEY (current_player_id) REFERENCES players(id)
);

CREATE INDEX IF NOT EXISTS idx_cross_season_map_player ON cross_season_map(current_player_id);
CREATE INDEX IF NOT EXISTS idx_cross_season_map_name ON cross_season_map(prior_player_name, season);
