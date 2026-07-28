package fetcher

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const vaastavBaseURL = "https://raw.githubusercontent.com/vaastav/Fantasy-Premier-League/master/data"

// VaastavFetcher downloads historical FPL data from the vaastav/Fantasy-Premier-League repo.
// Used to bootstrap cold-start predictions at season start when no current data exists.
type VaastavFetcher struct {
	client *http.Client
	seasons []string
}

// SeasonHistoryEntry holds a single player-gameweek record from a historical season.
type SeasonHistoryEntry struct {
	Season                 string
	PlayerName             string
	Position               string      // "GK", "DEF", "MID", "FWD"
	TeamName               string
	Event                  int
	Minutes                int
	TotalPoints            int
	GoalsScored            int
	Assists                int
	CleanSheets            int
	GoalsConceded          int
	ExpectedGoals          float64
	ExpectedAssists        float64
	ExpectedGoalInvolvements float64
	ExpectedGoalsConceded  float64
	Bonus                  int
	BPS                    int
	Influence              float64
	Creativity             float64
	Threat                 float64
	ICTIndex               float64
	WasHome                int
	OpponentTeam           string
	KickoffTime            string
	Value                  int
	Selected               int
}

// NewVaastavFetcher creates a new fetcher for the given seasons.
// seasons should be in format like ["2023-24", "2022-23"].
func NewVaastavFetcher(seasons []string) *VaastavFetcher {
	return &VaastavFetcher{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		seasons: seasons,
	}
}

// FetchAllSeasons downloads merged_gw.csv for each configured season.
func (vf *VaastavFetcher) FetchAllSeasons() ([]SeasonHistoryEntry, error) {
	var allEntries []SeasonHistoryEntry

	for _, season := range vf.seasons {
		entries, err := vf.fetchSeason(season)
		if err != nil {
			log.Printf("Warning: failed to fetch season %s: %v", season, err)
			continue
		}
		log.Printf("Fetched %d entries for season %s", len(entries), season)
		allEntries = append(allEntries, entries...)
	}

	return allEntries, nil
}

// fetchSeason downloads the merged_gw.csv for a specific season.
func (vf *VaastavFetcher) fetchSeason(season string) ([]SeasonHistoryEntry, error) {
	url := fmt.Sprintf("%s/%s/gws/merged_gw.csv", vaastavBaseURL, season)
	log.Printf("Fetching vaastav data: %s", url)

	resp, err := vf.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	return vf.parseCSV(resp.Body, season)
}

// parseCSV parses the merged_gw.csv format from vaastav repo.
// Columns: name,position,team,xP,assists,bonus,bps,clean_sheets,creativity,element,
//          expected_assists,expected_goal_involvements,expected_goals,expected_goals_conceded,
//          fixture,goals_conceded,goals_scored,ict_index,influence,kickoff_time,minutes,
//          opponent_team,own_goals,penalties_missed,penalties_saved,red_cards,round,saves,
//          selected,starts,team_a_score,team_h_score,threat,total_points,transfers_balance,
//          transfers_in,transfers_out,value,was_home,yellow_cards,GW
func (vf *VaastavFetcher) parseCSV(r io.Reader, season string) ([]SeasonHistoryEntry, error) {
	reader := csv.NewReader(r)

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	colIndex := buildColumnIndex(header)

	var entries []SeasonHistoryEntry

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Warning: CSV read error: %v, skipping row", err)
			continue
		}

		entry := SeasonHistoryEntry{
			Season: season,
		}

		if err := vf.mapRecord(record, colIndex, &entry); err != nil {
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// buildColumnIndex builds a map of column name → index from the CSV header.
func buildColumnIndex(header []string) map[string]int {
	index := make(map[string]int)
	for i, col := range header {
		index[strings.TrimSpace(col)] = i
	}
	return index
}

// mapRecord fills a SeasonHistoryEntry from a CSV record using the column index.
func (vf *VaastavFetcher) mapRecord(record []string, colIndex map[string]int, entry *SeasonHistoryEntry) error {
	get := func(col string) string {
		if idx, ok := colIndex[col]; ok && idx < len(record) {
			return strings.TrimSpace(record[idx])
		}
		return ""
	}

	entry.PlayerName = get("name")
	entry.Position = get("position")
	entry.TeamName = get("team")
	entry.OpponentTeam = get("opponent_team")
	entry.KickoffTime = get("kickoff_time")

	if v, err := strconv.Atoi(get("round")); err == nil {
		entry.Event = v
	}
	if v := get("GW"); v != "" {
		entry.Event, _ = strconv.Atoi(v)
	}

	entry.Minutes = parseIntSafe(get("minutes"))
	entry.TotalPoints = parseIntSafe(get("total_points"))
	entry.GoalsScored = parseIntSafe(get("goals_scored"))
	entry.Assists = parseIntSafe(get("assists"))
	entry.CleanSheets = parseIntSafe(get("clean_sheets"))
	entry.GoalsConceded = parseIntSafe(get("goals_conceded"))
	entry.Bonus = parseIntSafe(get("bonus"))
	entry.BPS = parseIntSafe(get("bps"))
	entry.Value = parseIntSafe(get("value"))
	entry.Selected = parseIntSafe(get("selected"))

	entry.ExpectedGoals = parseFloatSafe(get("expected_goals"))
	entry.ExpectedAssists = parseFloatSafe(get("expected_assists"))
	entry.ExpectedGoalInvolvements = parseFloatSafe(get("expected_goal_involvements"))
	entry.ExpectedGoalsConceded = parseFloatSafe(get("expected_goals_conceded"))
	entry.Influence = parseFloatSafe(get("influence"))
	entry.Creativity = parseFloatSafe(get("creativity"))
	entry.Threat = parseFloatSafe(get("threat"))
	entry.ICTIndex = parseFloatSafe(get("ict_index"))

	if v := get("was_home"); v == "True" || v == "true" {
		entry.WasHome = 1
	}

	return nil
}

func parseIntSafe(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func parseFloatSafe(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// GetLatestSeason returns the latest completed season for cold-start.
// If we're in 2025-26 season, the latest completed is 2024-25.
func GetEstimatedLatestSeason() []string {
	now := time.Now()
	year := now.Year()
	month := now.Month()

	// FPL season starts in August. If we're before August, latest completed is last season.
	if month < time.August {
		year--
	}

	seasons := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		s := fmt.Sprintf("%d-%d", year-1-i, (year-i)%100)
		seasons = append(seasons, s)
	}

	return seasons
}
