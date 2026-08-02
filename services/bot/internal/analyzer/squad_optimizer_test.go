package analyzer

import (
	"testing"

	"fpl-bot/internal/models"
)

// mkPlayers builds count synthetic players for a position, each on its own
// team (teamStart+i) so tests don't accidentally trip the max-3-per-team
// constraint unless they mean to.
func mkPlayers(pos, count, idStart, teamStart, costStart, costStep int, epStart, epStep float64, minutes int) []models.PlayerScore {
	var out []models.PlayerScore
	for i := 0; i < count; i++ {
		out = append(out, models.PlayerScore{
			PlayerID:              idStart + i,
			TeamID:                teamStart + i,
			Position:              pos,
			NowCost:               costStart + i*costStep,
			Availability:          1.0,
			Minutes:               minutes,
			ExpectedPointsMultiGW: epStart + float64(i)*epStep,
		})
	}
	return out
}

// amplePool returns a pool with plenty of supply at every position, well
// within DefaultSquadConstraints' £100m budget.
func amplePool() []models.PlayerScore {
	var all []models.PlayerScore
	all = append(all, mkPlayers(1, 6, 1, 1, 40, 2, 30, 1, 2500)...)       // GK
	all = append(all, mkPlayers(2, 10, 100, 11, 40, 3, 35, 2, 2500)...)   // DEF
	all = append(all, mkPlayers(3, 10, 200, 31, 45, 4, 40, 2.5, 2500)...) // MID
	all = append(all, mkPlayers(4, 8, 300, 51, 45, 5, 38, 3, 2500)...)    // FWD
	return all
}

func isValidFormation(f [3]int) bool {
	for _, vf := range validFormations {
		if vf == f {
			return true
		}
	}
	return false
}

func TestOptimizeSquadBest_AmplePool(t *testing.T) {
	squad := OptimizeSquadBest(amplePool(), DefaultSquadConstraints())
	if squad == nil {
		t.Fatal("expected a squad, got nil")
	}
	if squad.TotalCost > 1000 {
		t.Errorf("TotalCost = %d, want <= 1000", squad.TotalCost)
	}
	if len(squad.Goalkeepers) != 2 || len(squad.Defenders) != 5 ||
		len(squad.Midfielders) != 5 || len(squad.Forwards) != 3 {
		t.Errorf("squad composition = %d/%d/%d/%d, want 2/5/5/3",
			len(squad.Goalkeepers), len(squad.Defenders), len(squad.Midfielders), len(squad.Forwards))
	}
	if !isValidFormation(squad.Form) {
		t.Errorf("formation %v is not one of validFormations", squad.Form)
	}
}

func TestOptimizeSquadBest_NoForwards_ReturnsNil(t *testing.T) {
	var pool []models.PlayerScore
	pool = append(pool, mkPlayers(1, 6, 1, 1, 40, 2, 30, 1, 2500)...)
	pool = append(pool, mkPlayers(2, 10, 100, 11, 40, 3, 35, 2, 2500)...)
	pool = append(pool, mkPlayers(3, 10, 200, 31, 45, 4, 40, 2.5, 2500)...)
	// no forwards

	squad := OptimizeSquadBest(pool, DefaultSquadConstraints())
	if squad != nil {
		t.Fatalf("expected nil squad with no forwards available, got %+v", squad)
	}
}

func TestOptimizeSquadBest_EmptyPool_ReturnsNil(t *testing.T) {
	squad := OptimizeSquadBest(nil, DefaultSquadConstraints())
	if squad != nil {
		t.Fatalf("expected nil squad for empty pool, got %+v", squad)
	}
}

// A handful of DEF players all on the same team dominate on value; the
// optimizer must still cap at 3 from that team and fill the rest from
// elsewhere instead of either erroring or over-picking.
func TestOptimizeSquadBest_MaxThreePerTeam(t *testing.T) {
	const clusterTeam = 999
	var pool []models.PlayerScore
	pool = append(pool, mkPlayers(1, 6, 1, 1, 40, 2, 30, 1, 2500)...)
	pool = append(pool, mkPlayers(3, 10, 200, 31, 45, 4, 40, 2.5, 2500)...)
	pool = append(pool, mkPlayers(4, 8, 300, 51, 45, 5, 38, 3, 2500)...)

	// 5 cheap, high-EP defenders clustered on one team (best value by far).
	for i := 0; i < 5; i++ {
		pool = append(pool, models.PlayerScore{
			PlayerID:              400 + i,
			TeamID:                clusterTeam,
			Position:              2,
			NowCost:               40,
			Availability:          1.0,
			Minutes:               2500,
			ExpectedPointsMultiGW: 100,
		})
	}
	// Enough other defenders, spread one-per-team, to complete the line.
	pool = append(pool, mkPlayers(2, 10, 500, 601, 42, 3, 35, 2, 2500)...)

	squad := OptimizeSquadBest(pool, DefaultSquadConstraints())
	if squad == nil {
		t.Fatal("expected a squad, got nil")
	}
	teamCount := map[int]int{}
	for _, slot := range squad.allSlots() {
		teamCount[slot.Player.TeamID]++
	}
	if teamCount[clusterTeam] > 3 {
		t.Errorf("team %d has %d players in squad, want <= 3", clusterTeam, teamCount[clusterTeam])
	}
}

func TestOptimizeSeasonStartSquad_ExcludesLowMinutesStarter(t *testing.T) {
	constraints := SeasonStartSquadConstraints()

	starterQuality := amplePool()
	// Highest-EP player in the whole pool, but far below MinStarterMinutes.
	lowMinutesStar := models.PlayerScore{
		PlayerID:              999999,
		TeamID:                900,
		Position:              2,
		NowCost:               50,
		Availability:          1.0,
		Minutes:               100, // < MinStarterMinutes (1500) and < MinBenchMinutes (700)
		ExpectedPointsMultiGW: 9999,
	}
	starterQuality = append(starterQuality, lowMinutesStar)
	allPlayers := starterQuality

	squad := OptimizeSeasonStartSquad(starterQuality, allPlayers, constraints)
	if squad == nil {
		t.Fatal("expected a squad, got nil")
	}
	for _, slot := range squad.allSlots() {
		if slot.Player.PlayerID == lowMinutesStar.PlayerID {
			t.Fatalf("low-minutes player %d made the squad (starter=%v) despite Minutes=%d",
				lowMinutesStar.PlayerID, slot.IsStarter, lowMinutesStar.Minutes)
		}
	}
}

func TestOptimizeSeasonStartSquad_InsufficientCandidates_ReturnsNil(t *testing.T) {
	constraints := SeasonStartSquadConstraints()

	// Only 2 midfielders meet the bar; OptimizeSeasonStartSquadWithReserve
	// requires at least 3 before it even tries a formation.
	var starterQuality []models.PlayerScore
	starterQuality = append(starterQuality, mkPlayers(1, 2, 1, 1, 45, 2, 30, 1, 2500)...)
	starterQuality = append(starterQuality, mkPlayers(2, 5, 100, 11, 40, 3, 35, 2, 2500)...)
	starterQuality = append(starterQuality, mkPlayers(3, 2, 200, 31, 45, 4, 40, 2.5, 2500)...) // only 2 MID
	starterQuality = append(starterQuality, mkPlayers(4, 3, 300, 51, 45, 5, 38, 3, 2500)...)

	squad := OptimizeSeasonStartSquad(starterQuality, starterQuality, constraints)
	if squad != nil {
		t.Fatalf("expected nil squad with only 2 MID candidates, got %+v", squad)
	}
}
