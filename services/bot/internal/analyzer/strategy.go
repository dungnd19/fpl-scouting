package analyzer

import (
	"math"
	"sort"

	"fpl-bot/internal/models"
)

// StrategyConfig holds parameters for multi-week strategy optimization.
type StrategyConfig struct {
	MaxGameweeks   int     // number of gameweeks to look ahead (default 3)
	DiscountFactor float64 // exponential discount base (default 14/15)
	MaxFreeTransfers int   // maximum accumulated free transfers (default 5)
}

// DefaultStrategyConfig returns sensible default parameters.
func DefaultStrategyConfig() StrategyConfig {
	return StrategyConfig{
		MaxGameweeks:     3,
		DiscountFactor:   14.0 / 15.0,
		MaxFreeTransfers: 5,
	}
}

// StrategyNode represents a decision point in the multi-GW transfer tree.
type StrategyNode struct {
	Gameweek       int
	TransfersUsed  int
	FreeTransfers  int // available free transfers at this node
	ChipPlayed     string
	PointsHit      int
	SquadPoints    float64
	CumulativePts  float64
	Children       []*StrategyNode
}

// EvaluateTransferStrategy computes the optimal transfer strategy over N gameweeks.
//
// For each gameweek, it evaluates 3 options: 0 transfers, 1 transfer, 2 transfers.
// For 2+ transfers: points hit = 4 * max(0, transfers - free_transfers_available).
// Free transfers accumulate by 1 each week (capped at MaxFreeTransfers).
// Future gameweeks are discounted exponentially: discount^(gw - currentGW).
//
// Returns the best strategy path and its total discounted points.
func EvaluateTransferStrategy(
	squadScores []SquadGameweekScore,
	currentGW int,
	cfg StrategyConfig,
) ([]models.TransferPlan, float64) {
	if cfg.MaxGameweeks <= 0 {
		cfg = DefaultStrategyConfig()
	}

	root := &StrategyNode{
		Gameweek:      currentGW,
		TransfersUsed: 0,
		FreeTransfers: cfg.MaxFreeTransfers,
	}

	bestPaths := make([]models.TransferPlan, 0, cfg.MaxGameweeks)
	buildTree(root, squadScores, currentGW, cfg)

	totalDiscounted := findBestPath(root, &bestPaths)

	return bestPaths, totalDiscounted
}

// SquadGameweekScore pairs a gameweek with expected squad points after transfers.
type SquadGameweekScore struct {
	Gameweek        int
	CurrentPoints   float64 // squad points with 0 transfers
	PointsAfter1FT  float64 // squad points after 1 free transfer
	PointsAfter2FT  float64 // squad points after 2 free transfers (possibly with hit)
}

// buildTree recursively builds the strategy tree.
func buildTree(
	node *StrategyNode,
	squadScores []SquadGameweekScore,
	currentGW int,
	cfg StrategyConfig,
) {
	gwIdx := currentGW - squadScores[0].Gameweek
	if gwIdx < 0 {
		return
	}

	gwsRemaining := len(squadScores) - gwIdx
	if gwsRemaining <= 0 {
		return
	}

	ss := squadScores[gwIdx]

	maxTransfers := node.FreeTransfers + 1
	if maxTransfers > 2 {
		maxTransfers = 2
	}

	for numTransfers := 0; numTransfers <= maxTransfers; numTransfers++ {
		var squadPts float64
		switch numTransfers {
		case 0:
			squadPts = ss.CurrentPoints
		case 1:
			squadPts = ss.PointsAfter1FT
		case 2:
			squadPts = ss.PointsAfter2FT
		}

		freeAfter := node.FreeTransfers - numTransfers
		pointsHit := 0
		if freeAfter < 0 {
			pointsHit = -freeAfter * 4
			freeAfter = 0
		}

		nextFree := freeAfter + 1
		if nextFree > cfg.MaxFreeTransfers {
			nextFree = cfg.MaxFreeTransfers
		}

		// Single-gameweek discount
		gamesAhead := ss.Gameweek - currentGW
		discount := math.Pow(cfg.DiscountFactor, float64(gamesAhead))
		netPoints := (squadPts - float64(pointsHit)) * discount

		child := &StrategyNode{
			Gameweek:      ss.Gameweek,
			TransfersUsed: numTransfers,
			FreeTransfers: nextFree,
			PointsHit:     pointsHit,
			SquadPoints:   netPoints,
		}

		if gwsRemaining > 1 && gwIdx+1 < len(squadScores) {
			buildTree(child, squadScores, ss.Gameweek+1, cfg)
		}

		node.Children = append(node.Children, child)
	}
}

// findBestPath traverses the tree and returns the best cumulative path.
func findBestPath(node *StrategyNode, plans *[]models.TransferPlan) float64 {
	if len(node.Children) == 0 {
		return node.SquadPoints
	}

	bestTotal := -999999.0
	var bestChild *StrategyNode

	for i := range node.Children {
		child := node.Children[i]
		childTotal := findBestPath(child, plans)
		totalForChild := node.SquadPoints + childTotal

		if totalForChild > bestTotal {
			bestTotal = totalForChild
			bestChild = child
		}
	}

	if bestChild != nil && node.Gameweek == bestChild.Gameweek-1 {
		plan := models.TransferPlan{
			Gameweek:      bestChild.Gameweek,
			TransfersOut:  nil,
			TransfersIn:  nil,
			FreeTransfers: bestChild.FreeTransfers - 1,
			PointsHit:     bestChild.PointsHit,
		}
		if bestChild.TransfersUsed > 0 {
			plan.FreeTransfers = bestChild.FreeTransfers - 1
		}
		*plans = append(*plans, plan)
	}

	if node.TransfersUsed > 0 {
		return bestTotal
	}
	return bestTotal
}

// ComputeTransferGain computes net gain of a transfer accounting for points hit.
// gain = (buyEP - sellEP) * multiGWDuration - pointsHit
// Where multiGWDuration accounts for the number of gameweeks the transfer is active.
func ComputeTransferGain(sellEP, buyEP float64, freeTransfers, numTransfers int, multiGW int) float64 {
	rawGain := (buyEP - sellEP) * float64(multiGW)

	usedFree := numTransfers
	if usedFree > freeTransfers {
		usedFree = freeTransfers
	}
	extraTransfers := numTransfers - usedFree
	if extraTransfers < 0 {
		extraTransfers = 0
	}
	pointsHit := extraTransfers * 4

	return rawGain - float64(pointsHit)
}

// OptimizeSingleWeekTransfers finds the best 0/1/2-transfer combination for a single gameweek.
// This is the simplest useful version of the strategy — evaluate all single/double transfers
// and compute the net expected points gain accounting for free transfers.
//
// Returns sorted recommendations with net gain (accounting for points hit).
func OptimizeSingleWeekTransfers(
	candidates []models.Recommendation,
	freeTransfers int,
	multiGWValue int, // number of GWs the player helps (1 = only next GW)
) []models.Recommendation {
	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	var result []models.Recommendation

	// Evaluate best single transfer
	if len(candidates) > 0 {
		best := candidates[0]
		gain := ComputeTransferGain(0, best.ExpectedGain, freeTransfers, 1, multiGWValue)
		best.Score = gain
		result = append(result, best)
	}

	// Evaluate best double transfer combo
	if len(candidates) >= 2 {
		a, b := candidates[0], candidates[1]
		if a.SellPlayerID != b.SellPlayerID {
			combinedGain := ComputeTransferGain(
				0, a.ExpectedGain+b.ExpectedGain, freeTransfers, 2, multiGWValue,
			)
			a.Score = combinedGain
			a.Reason = a.Reason + " [pair with: " + b.BuyPlayerName + " for " + b.BuyPlayerName + "]"
			result = append(result, a)
		}
	}

	// No transfer option — baseline
	baseline := models.Recommendation{
		Score:   0,
		Reason:  "No transfer (save FT)",
	}
	result = append(result, baseline)

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}
