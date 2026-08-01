package analyzer

import (
	"math"
)

// FPL scoring constants — position-specific
var goalPoints = map[int]float64{1: 10, 2: 6, 3: 5, 4: 4}
var csPoints = map[int]float64{1: 4, 2: 4, 3: 1, 4: 0}

// appearancePoints returns points for minutes played: 1pt for appearing, +1pt for >=60 mins
func appearancePoints(minutes float64) float64 {
	if minutes <= 0 {
		return 0
	}
	pts := 1.0
	if minutes >= 60 {
		pts = 2.0
	}
	return pts
}

// avgBonusPerAppearance returns average bonus points per appearance for a given position.
// Based on typical historical averages from FPL data.
func avgBonusPerAppearance(position int) float64 {
	switch position {
	case 1:
		return 0.15
	case 2:
		return 0.25
	case 3:
		return 0.35
	case 4:
		return 0.40
	default:
		return 0.25
	}
}

// avgCardsPerAppearance returns average card-related points loss per appearance.
func avgCardsPerAppearance(position int) float64 {
	switch position {
	case 1:
		return -0.03
	case 2:
		return -0.15
	case 3:
		return -0.10
	case 4:
		return -0.05
	default:
		return -0.10
	}
}

// SimpleExpectedPoints is a faster approximation of expected FPL points without multinomial.
// Uses linear approximation: xG * goal_pts + xA * assist_pts + csRate * cs_pts + appearance.
// This is the compromise between accuracy and performance for quick calculations.
func SimpleExpectedPoints(
	position int,
	xGPer90 float64,
	xAPer90 float64,
	xGCPer90 float64,
	csRate float64,
	ppg float64,
	avail float64,
	expectedMinutes float64,
	fixtureDifficulty float64,
	defconScore float64,
) float64 {
	if expectedMinutes <= 0 || avail <= 0 {
		return 0
	}

	minsFraction := expectedMinutes / 90.0
	if minsFraction > 1.0 {
		minsFraction = 1.0
	}

	ep := 0.0

	// Appearance
	ep += appearancePoints(expectedMinutes) * avail

	// Attacking — linear approximation
	gp := goalPoints[position]
	if gp == 0 {
		gp = 4
	}
	adjXG := xGPer90 * minsFraction * fixtureDifficulty
	adjXA := xAPer90 * minsFraction * fixtureDifficulty
	ep += adjXG*gp + adjXA*3.0

	// Defending
	if position != 4 {
		if minsFraction >= 60.0/90.0 {
			adjCS := csRate * fixtureDifficulty
			ep += csPoints[position] * adjCS
		}
		if position == 1 || position == 2 {
			adjXGC := xGCPer90 * minsFraction / fixtureDifficulty
			ep -= adjXGC * 0.5
		}
	}

	// Bonus + cards
	ep += avgBonusPerAppearance(position) * avail
	ep += avgCardsPerAppearance(position) * avail

	// DEFCON points
	ep += defconScore

	return ep
}

// MultiGWExpectedPoints computes discounted expected points over N upcoming gameweeks.
// Uses exponential discount: (14/15)^(games_ahead)
// This accounts for fixture strength variation across the lookahead window.
func MultiGWExpectedPoints(
	position int,
	xGPer90 float64,
	xAPer90 float64,
	csRate float64,
	ppg float64,
	avail float64,
	expectedMinutes float64,
	fixtures []FixtureWithDifficulty,
	defconScore float64,
) float64 {
	total := 0.0
	for _, f := range fixtures {
		discount := math.Pow(14.0/15.0, float64(f.GamesAhead))
		fdm := FixtureDifficultyMultiplier(f.Difficulty)
		ep := SimpleExpectedPoints(
			position, xGPer90, xAPer90, 1.0, csRate, ppg, avail, expectedMinutes, fdm, defconScore,
		)
		total += ep * discount
	}
	return total
}

type FixtureWithDifficulty struct {
	Gameweek   int
	IsHome     bool
	Difficulty int
	GamesAhead int
}
