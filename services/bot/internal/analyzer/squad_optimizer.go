package analyzer

import (
	"log"
	"math"
	"sort"
	"strconv"

	"fpl-bot/internal/models"
)

type SquadSlot struct {
	Player    models.PlayerScore
	IsStarter bool
}

type OptimizedSquad struct {
	Goalkeepers  []SquadSlot
	Defenders    []SquadSlot
	Midfielders  []SquadSlot
	Forwards     []SquadSlot
	TotalCost    int
	TotalEP      float64
	Bank         int
	Form         [3]int
	BenchReserve int
	TeamEP       float64
}

type SquadConstraints struct {
	Budget100k        int
	MaxPerTeam        int
	NumGK             int
	NumDEF            int
	NumMID            int
	NumFWD            int
	MinStarterMinutes int
	MinBenchMinutes   int
}

func DefaultSquadConstraints() SquadConstraints {
	return SquadConstraints{
		Budget100k: 1000,
		MaxPerTeam: 3,
		NumGK:      2,
		NumDEF:     5,
		NumMID:     5,
		NumFWD:     3,
	}
}

func SeasonStartSquadConstraints() SquadConstraints {
	return SquadConstraints{
		Budget100k:        1000,
		MaxPerTeam:        3,
		NumGK:             2,
		NumDEF:            5,
		NumMID:            5,
		NumFWD:            3,
		MinStarterMinutes: 1500,
		MinBenchMinutes:   700,
	}
}

// Valid FPL formations (at least 3 midfielders required)
var validFormations = [][3]int{
	{3, 4, 3}, {3, 5, 2}, {4, 3, 3}, {4, 4, 2},
	{4, 5, 1}, {5, 3, 2}, {5, 4, 1},
}

// OptimizeSquadWithBenchReserve builds a squad for a specific bench budget and returns it.
func OptimizeSquadWithBenchReserve(players []models.PlayerScore, constraints SquadConstraints, benchReserve int) *OptimizedSquad {
	if constraints.Budget100k == 0 {
		constraints = DefaultSquadConstraints()
	}

	// Filter to available players with sufficient data
	var available []models.PlayerScore
	for _, p := range players {
		if p.Availability > 0 && p.NowCost > 0 {
			ep := p.ExpectedPointsMultiGW
			if ep <= 0 {
				ep = p.ExpectedPoints
			}
			if ep > 0 {
				p.OverallScore = ep
				available = append(available, p)
			}
		}
	}

	byPos := map[int][]models.PlayerScore{}
	for _, p := range available {
		byPos[p.Position] = append(byPos[p.Position], p)
	}
	for pos := range byPos {
		sort.Slice(byPos[pos], func(i, j int) bool {
			return byPos[pos][i].OverallScore > byPos[pos][j].OverallScore
		})
	}

	bestEP := -1.0
	var bestSquad *OptimizedSquad

	for _, formation := range validFormations {
		defN, midN, fwdN := formation[0], formation[1], formation[2]
		squad := tryFormation(byPos, constraints, defN, midN, fwdN, benchReserve)
		if squad == nil {
			continue
		}
		ep := squad.starting11EP()
		if ep > bestEP {
			bestEP = ep
			bestSquad = squad
			bestSquad.Form = formation
			bestSquad.BenchReserve = benchReserve
		}
	}

	if bestSquad != nil {
		bestSquad.applyFormation(bestSquad.Form)
		bestSquad.computeCosts()
	}
	return bestSquad
}

// OptimizeSquadBest runs a sweep of bench reserves from 200 down to 120
// and returns the best starting 11 across all bench levels.
func OptimizeSquadBest(players []models.PlayerScore, constraints SquadConstraints) *OptimizedSquad {
	bestEP := -1.0
	var best *OptimizedSquad

	for benchR := 200; benchR >= 30; benchR -= 10 {
		squad := OptimizeSquadWithBenchReserve(players, constraints, benchR)
		if squad == nil {
			continue
		}
		ep := squad.Starting11EP()
		if ep > bestEP {
			bestEP = ep
			best = squad
		}
	}
	return best
}

func OptimizeSquad(players []models.PlayerScore, constraints SquadConstraints) *OptimizedSquad {
	return OptimizeSquadBest(players, constraints)
}

func OptimizeSquadMultiRun(players []models.PlayerScore, constraints SquadConstraints, runs int) *OptimizedSquad {
	return OptimizeSquadBest(players, constraints)
}

func tryFormation(byPos map[int][]models.PlayerScore, c SquadConstraints, defN, midN, fwdN int, benchReserve int) *OptimizedSquad {
	picked := map[int]bool{}
	teamCnt := map[int]int{}
	squad := &OptimizedSquad{}
	cost := 0

	// 1. Fill bench first with cheapest per position (FWD→MID→DEF→GK, most expensive first)
	benchNeeded := map[int]int{
		1: c.NumGK - 1,
		2: c.NumDEF - defN,
		3: c.NumMID - midN,
		4: c.NumFWD - fwdN,
	}

	for _, pos := range []int{4, 3, 2, 1} {
		if benchNeeded[pos] <= 0 {
			continue
		}
		slotList := squad.slotListForPos(pos)
		cheapest := make([]models.PlayerScore, len(byPos[pos]))
		copy(cheapest, byPos[pos])
		sort.Slice(cheapest, func(i, j int) bool {
			return cheapest[i].NowCost < cheapest[j].NowCost
		})
		for _, p := range cheapest {
			if benchNeeded[pos] <= 0 {
				break
			}
			if picked[p.PlayerID] || teamCnt[p.TeamID] >= c.MaxPerTeam {
				continue
			}
			if cost+p.NowCost > c.Budget100k {
				continue
			}
			picked[p.PlayerID] = true
			teamCnt[p.TeamID]++
			cost += p.NowCost
			*slotList = append(*slotList, SquadSlot{Player: p, IsStarter: false})
			benchNeeded[pos]--
		}
		if benchNeeded[pos] > 0 {
			return nil
		}
	}

	// 2. Fill starters with remaining budget, interleaved value-based across positions
	needed := map[int]int{1: 1, 2: defN, 3: midN, 4: fwdN}
	posIdx := map[int]int{1: 0, 2: 0, 3: 0, 4: 0}

	for {
		bestPos := -1
		bestVal := -1.0
		bestPlayer := models.PlayerScore{}
		bestIdx := -1

		for _, pos := range []int{1, 2, 3, 4} {
			if needed[pos] <= 0 {
				continue
			}
			for idx := posIdx[pos]; idx < len(byPos[pos]); idx++ {
				p := byPos[pos][idx]
				if picked[p.PlayerID] || teamCnt[p.TeamID] >= c.MaxPerTeam {
					continue
				}
				if cost+p.NowCost > c.Budget100k {
					continue
				}
				val := p.OverallScore / float64(p.NowCost)
				if val > bestVal {
					bestVal = val
					bestPos = pos
					bestPlayer = p
					bestIdx = idx
				}
				break
			}
		}

		if bestPos < 0 {
			break
		}

		picked[bestPlayer.PlayerID] = true
		teamCnt[bestPlayer.TeamID]++
		cost += bestPlayer.NowCost
		needed[bestPos]--

		slotList := squad.slotListForPos(bestPos)
		*slotList = append(*slotList, SquadSlot{Player: bestPlayer, IsStarter: true})
		posIdx[bestPos] = bestIdx + 1
	}

	if len(squad.Goalkeepers) < 1 || len(squad.Defenders) < defN ||
		len(squad.Midfielders) < midN || len(squad.Forwards) < fwdN {
		return nil
	}

	return squad
}

func (s *OptimizedSquad) applyFormation(f [3]int) {
	for _, list := range []*[]SquadSlot{&s.Defenders, &s.Midfielders, &s.Forwards, &s.Goalkeepers} {
		sort.Slice(*list, func(i, j int) bool {
			return (*list)[i].Player.OverallScore > (*list)[j].Player.OverallScore
		})
	}
	for i := range s.Goalkeepers {
		s.Goalkeepers[i].IsStarter = i == 0
	}
	for i := range s.Defenders {
		s.Defenders[i].IsStarter = i < f[0]
	}
	for i := range s.Midfielders {
		s.Midfielders[i].IsStarter = i < f[1]
	}
	for i := range s.Forwards {
		s.Forwards[i].IsStarter = i < f[2]
	}
}

func (s *OptimizedSquad) applyFormationSeasonStart(f [3]int) {
	sortAndSeparate := func(list *[]SquadSlot, starterCount int) {
		var starters, bench []SquadSlot
		for _, slot := range *list {
			if slot.IsStarter {
				starters = append(starters, slot)
			} else {
				bench = append(bench, slot)
			}
		}
		sort.Slice(starters, func(i, j int) bool {
			return starters[i].Player.OverallScore > starters[j].Player.OverallScore
		})
		sort.Slice(bench, func(i, j int) bool {
			return bench[i].Player.OverallScore > bench[j].Player.OverallScore
		})
		*list = append(starters, bench...)
		for i := range *list {
			(*list)[i].IsStarter = i < starterCount
		}
	}
	sortAndSeparate(&s.Goalkeepers, 1)
	sortAndSeparate(&s.Defenders, f[0])
	sortAndSeparate(&s.Midfielders, f[1])
	sortAndSeparate(&s.Forwards, f[2])
}

func (s *OptimizedSquad) computeCosts() {
	s.TotalCost = 0
	for _, slot := range s.allSlots() {
		s.TotalCost += slot.Player.NowCost
	}
	s.Bank = 1000 - s.TotalCost
}

func (s *OptimizedSquad) starting11EP() float64 {
	total := 0.0
	for _, slot := range s.allSlots() {
		if slot.IsStarter {
			total += slot.Player.OverallScore
		}
	}
	return total
}

func (s *OptimizedSquad) Starting11EP() float64 { return s.starting11EP() }

func (s *OptimizedSquad) Formation() string {
	return strconv.Itoa(s.Form[0]) + "-" + strconv.Itoa(s.Form[1]) + "-" + strconv.Itoa(s.Form[2])
}

func (s *OptimizedSquad) allSlots() []SquadSlot {
	var all []SquadSlot
	all = append(all, s.Goalkeepers...)
	all = append(all, s.Defenders...)
	all = append(all, s.Midfielders...)
	all = append(all, s.Forwards...)
	return all
}

func (s *OptimizedSquad) slotListForPos(pos int) *[]SquadSlot {
	switch pos {
	case 1:
		return &s.Goalkeepers
	case 2:
		return &s.Defenders
	case 3:
		return &s.Midfielders
	case 4:
		return &s.Forwards
	}
	return nil
}

func RoundTo(val float64, places int) float64 {
	pow := math.Pow(10, float64(places))
	return math.Round(val*pow) / pow
}

func (s *OptimizedSquad) teamEP() float64 {
	total := 0.0
	for _, slot := range s.allSlots() {
		total += slot.Player.OverallScore
	}
	return total
}

func (s *OptimizedSquad) TeamEPValue() float64 { return s.TeamEP }

func tryFormationSeasonStart(
	startersByPos map[int][]models.PlayerScore,
	benchByPos map[int][]models.PlayerScore,
	c SquadConstraints,
	defN, midN, fwdN int,
	benchReserve int,
) *OptimizedSquad {
	picked := map[int]bool{}
	teamCnt := map[int]int{}
	squad := &OptimizedSquad{}
	cost := 0

	// 1. Fill bench first with cheapest + minute-qualified per position
	benchNeeded := map[int]int{
		1: c.NumGK - 1,
		2: c.NumDEF - defN,
		3: c.NumMID - midN,
		4: c.NumFWD - fwdN,
	}

	for _, pos := range []int{4, 3, 2, 1} {
		if benchNeeded[pos] <= 0 {
			continue
		}
		slotList := squad.slotListForPos(pos)
		cheapest := make([]models.PlayerScore, len(benchByPos[pos]))
		copy(cheapest, benchByPos[pos])
		sort.Slice(cheapest, func(i, j int) bool {
			return cheapest[i].NowCost < cheapest[j].NowCost
		})
		for _, p := range cheapest {
			if benchNeeded[pos] <= 0 {
				break
			}
			if picked[p.PlayerID] || teamCnt[p.TeamID] >= c.MaxPerTeam {
				continue
			}
			if p.Minutes < c.MinBenchMinutes {
				continue
			}
			if cost+p.NowCost > c.Budget100k {
				continue
			}
			picked[p.PlayerID] = true
			teamCnt[p.TeamID]++
			cost += p.NowCost
			*slotList = append(*slotList, SquadSlot{Player: p, IsStarter: false})
			benchNeeded[pos]--
		}
		if benchNeeded[pos] > 0 {
			return nil
		}
	}

	// 2. Fill starters with remaining budget, interleaved value-based
	needed := map[int]int{1: 1, 2: defN, 3: midN, 4: fwdN}
	posIdx := map[int]int{1: 0, 2: 0, 3: 0, 4: 0}

	for {
		bestPos := -1
		bestVal := -1.0
		bestPlayer := models.PlayerScore{}
		bestIdx := -1

		for _, pos := range []int{1, 2, 3, 4} {
			if needed[pos] <= 0 {
				continue
			}
			pool := startersByPos[pos]
			for idx := posIdx[pos]; idx < len(pool); idx++ {
				p := pool[idx]
				if picked[p.PlayerID] || teamCnt[p.TeamID] >= c.MaxPerTeam {
					continue
				}
				if p.Minutes < c.MinStarterMinutes {
					continue
				}
				if cost+p.NowCost > c.Budget100k {
					continue
				}
				val := p.OverallScore / float64(p.NowCost)
				if val > bestVal {
					bestVal = val
					bestPos = pos
					bestPlayer = p
					bestIdx = idx
				}
				break
			}
		}

		if bestPos < 0 {
			break
		}

		picked[bestPlayer.PlayerID] = true
		teamCnt[bestPlayer.TeamID]++
		cost += bestPlayer.NowCost
		needed[bestPos]--

		slotList := squad.slotListForPos(bestPos)
		*slotList = append(*slotList, SquadSlot{Player: bestPlayer, IsStarter: true})
		posIdx[bestPos] = bestIdx + 1
	}

	if len(squad.Goalkeepers) < 1 || len(squad.Defenders) < defN ||
		len(squad.Midfielders) < midN || len(squad.Forwards) < fwdN {
		return nil
	}

	squad.TotalCost = cost
	squad.Bank = c.Budget100k - cost
	squad.Form = [3]int{defN, midN, fwdN}
	squad.BenchReserve = benchReserve
	squad.TeamEP = squad.teamEP()
	return squad
}

func OptimizeSeasonStartSquadWithReserve(
	starterQuality []models.PlayerScore,
	allPlayers []models.PlayerScore,
	constraints SquadConstraints,
	benchReserve int,
) *OptimizedSquad {
	if constraints.Budget100k == 0 {
		constraints = SeasonStartSquadConstraints()
	}
	if constraints.MinStarterMinutes == 0 {
		constraints.MinStarterMinutes = 1500
	}
	if constraints.MinBenchMinutes == 0 {
		constraints.MinBenchMinutes = 700
	}

	startersByPos := buildPosMap(starterQuality, true)
	benchByPos := buildPosMap(allPlayers, false)

	if len(startersByPos[1]) < 1 || len(startersByPos[2]) < 3 || len(startersByPos[3]) < 3 || len(startersByPos[4]) < 1 {
		log.Printf("SeasonStartSquad: insufficient starter candidates: GK=%d DEF=%d MID=%d FWD=%d",
			len(startersByPos[1]), len(startersByPos[2]), len(startersByPos[3]), len(startersByPos[4]))
		return nil
	}
	log.Printf("SeasonStartSquad: benchR=%d starters GK=%d DEF=%d MID=%d FWD=%d | bench GK=%d DEF=%d MID=%d FWD=%d",
		benchReserve, len(startersByPos[1]), len(startersByPos[2]), len(startersByPos[3]), len(startersByPos[4]),
		len(benchByPos[1]), len(benchByPos[2]), len(benchByPos[3]), len(benchByPos[4]))

	bestEP := -1.0
	var bestSquad *OptimizedSquad

	for _, formation := range validFormations {
		defN, midN, fwdN := formation[0], formation[1], formation[2]
		squad := tryFormationSeasonStart(startersByPos, benchByPos, constraints, defN, midN, fwdN, benchReserve)
		if squad == nil {
			continue
		}
		ep := squad.starting11EP()
		if ep > bestEP {
			bestEP = ep
			bestSquad = squad
		}
	}

	if bestSquad != nil {
		bestSquad.applyFormationSeasonStart(bestSquad.Form)
		bestSquad.TotalEP = bestSquad.starting11EP()
	}
	return bestSquad
}

func OptimizeSeasonStartSquad(
	starterQuality []models.PlayerScore,
	allPlayers []models.PlayerScore,
	constraints SquadConstraints,
) *OptimizedSquad {
	bestEP := -1.0
	var best *OptimizedSquad

	for benchR := 200; benchR >= 30; benchR -= 10 {
		squad := OptimizeSeasonStartSquadWithReserve(starterQuality, allPlayers, constraints, benchR)
		if squad == nil {
			continue
		}
		ep := squad.Starting11EP()
		if ep > bestEP {
			bestEP = ep
			best = squad
		}
	}
	return best
}

func buildPosMap(players []models.PlayerScore, filterByEP bool) map[int][]models.PlayerScore {
	byPos := map[int][]models.PlayerScore{}
	for _, p := range players {
		if p.Availability <= 0 || p.NowCost <= 0 {
			continue
		}
		ep := p.ExpectedPointsMultiGW
		if ep <= 0 {
			ep = p.ExpectedPoints
		}
		if filterByEP && ep <= 0 {
			continue
		}
		p.OverallScore = ep
		byPos[p.Position] = append(byPos[p.Position], p)
	}
	for pos := range byPos {
		sort.Slice(byPos[pos], func(i, j int) bool {
			return byPos[pos][i].OverallScore > byPos[pos][j].OverallScore
		})
	}
	return byPos
}

func FilterPlayersByMinutes(allPlayers []models.PlayerScore, minMinutes int) []models.PlayerScore {
	var filtered []models.PlayerScore
	for _, p := range allPlayers {
		if p.Minutes >= minMinutes {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
