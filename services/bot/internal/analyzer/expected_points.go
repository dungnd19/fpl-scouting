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

// ExpectedFPLPoints computes the true expected FPL points for a player in an upcoming fixture.
// This replaces the heuristic xScore with actual FPL scoring rules.
//
// Components:
//   E[appearance] = P(plays) * appearance_points(minutes)
//   E[attacking] = multinomial expectation over team goal distribution
//   E[defending] = P(CS) * cs_points[position] - concede_penalty
//   E[bonus] = average bonus per appearance (from historical data)
//   E[cards] = average cards per appearance (negative)
func ExpectedFPLPoints(
	position int,
	xGPer90 float64,
	xAPer90 float64,
	xGCPer90 float64,
	csRate float64,
	ppg float64,
	avail float64,
	expectedMinutes float64,
	teamGoalDist []float64,
	teamConcedeDist []float64,
) float64 {
	if expectedMinutes <= 0 || avail <= 0 {
		return 0
	}

	minsFraction := expectedMinutes / 90.0
	if minsFraction > 1.0 {
		minsFraction = 1.0
	}

	ep := 0.0

	// 1. Appearance points
	ep += appearancePoints(expectedMinutes) * avail

	// 2. Attacking points — multinomial expectation
	ep += attackingPointsMultinomial(position, xGPer90, xAPer90, minsFraction, teamGoalDist)

	// 3. Defending points
	if position != modelsPosFWD() {
		ep += defendingPoints(position, minsFraction, csRate, xGCPer90, teamConcedeDist)
	}

	// 4. Bonus points — average historical bonus per game, scaled by availability
	bonusAvg := avgBonusPerAppearance(position)
	ep += bonusAvg * avail

	// 5. Card points — average historical cards
	cardAvg := avgCardsPerAppearance(position)
	ep += cardAvg * avail

	return ep
}

// attackingPointsMultinomial computes expected attacking points using the multinomial distribution.
//
// For each possible team goal count n (from teamGoalDist), compute:
//
//	E[pts | team scores n] = Σ_{g=0}^n Σ_{a=0}^{n-g} multinomial_pmf(g, a, n-g-a | probs) * (g*goal_pts + a*assist_pts)
//
// where probs = (prScore, prAssist, prNeither)
func attackingPointsMultinomial(
	position int,
	xGPer90 float64,
	xAPer90 float64,
	minsFraction float64,
	teamGoalDist []float64,
) float64 {
	prScore := xGPer90 * minsFraction
	prAssist := xAPer90 * minsFraction
	if prScore+prAssist > 1.0 {
		norm := prScore + prAssist
		prScore /= norm
		prAssist /= norm
	}
	prNeither := 1.0 - prScore - prAssist
	if prNeither < 0 {
		prNeither = 0
		norm := prScore + prAssist
		if norm > 0 {
			prScore /= norm
			prAssist /= norm
			prNeither = 0
		}
	}

	gp := goalPoints[position]
	if gp == 0 {
		gp = 4
	}
	ap := 3.0

	totalExp := 0.0

	for n, probN := range teamGoalDist {
		if probN <= 0 || n == 0 {
			continue
		}
		expGivenN := 0.0
		for g := 0; g <= n; g++ {
			for a := 0; a <= n-g; a++ {
				neq := n - g - a
				p := multinomialPMF(g, a, neq, n, prScore, prAssist, prNeither)
				pointsFromPartition := float64(g)*gp + float64(a)*ap
				expGivenN += p * pointsFromPartition
			}
		}
		totalExp += expGivenN * probN
	}

	return totalExp
}

// multinomialPMF computes P(g, a, neq) given n and probabilities.
func multinomialPMF(g, a, neq, n int, pScore, pAssist, pNeither float64) float64 {
	if n == 0 {
		return 0
	}
	logResult := logFactorial(n) - logFactorial(g) - logFactorial(a) - logFactorial(neq)
	logResult += float64(g)*math.Log(maxSmall(pScore, 1e-15)) +
		float64(a)*math.Log(maxSmall(pAssist, 1e-15)) +
		float64(neq)*math.Log(maxSmall(pNeither, 1e-15))
	result := math.Exp(logResult)
	if result < 0 || math.IsNaN(result) {
		return 0
	}
	return result
}

// logFactorial returns ln(n!) using precomputed values for small n.
func logFactorial(n int) float64 {
	if n <= 1 {
		return 0
	}
	if n < len(logFactCache) {
		return logFactCache[n]
	}
	result := 0.0
	for i := 2; i <= n; i++ {
		result += math.Log(float64(i))
	}
	return result
}

var logFactCache = func() []float64 {
	cache := make([]float64, 21)
	for i := 1; i <= 20; i++ {
		cache[i] = logFactorialSlow(i)
	}
	return cache
}()

func logFactorialSlow(n int) float64 {
	result := 0.0
	for i := 2; i <= n; i++ {
		result += math.Log(float64(i))
	}
	return result
}

// defendingPoints computes expected defending points.
// Clean sheet: 4pts for GK/DEF, 1pt for MID, 0 for FWD (requires >=60 mins)
// Concession: -1pt per 2 goals conceded for GK/DEF only
func defendingPoints(
	position int,
	minsFraction float64,
	csRate float64,
	xGCPer90 float64,
	teamConcedeDist []float64,
) float64 {
	pts := 0.0

	if minsFraction >= 60.0/90.0 {
		pts += csPoints[position] * csRate
	}

	if position == 1 || position == 2 {
		concedePenalty := 0.0
		for n, prob := range teamConcedeDist {
			if n >= 2 {
				concedePenalty += float64(n/2) * minsFraction * prob
			}
		}
		pts -= concedePenalty
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
) float64 {
	total := 0.0
	for _, f := range fixtures {
		discount := math.Pow(14.0/15.0, float64(f.GamesAhead))
		fdm := FixtureDifficultyMultiplier(f.Difficulty)
		ep := SimpleExpectedPoints(
			position, xGPer90, xAPer90, 1.0, csRate, ppg, avail, expectedMinutes, fdm,
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

func modelsPosFWD() int { return 4 }

func maxSmall(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
