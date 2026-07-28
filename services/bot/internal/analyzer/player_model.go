package analyzer

import (
	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

// BayesianPlayerModel fits per-position Dirichlet priors using empirical Bayes,
// then computes posterior probability of goal involvement for each player.
//
// Prior:  Dirichlet(alpha_G, alpha_A, alpha_N)
//   where alpha comes from position-wide averages of goals/assists per team goal.
//   Prior strength: nGoalsPrior = 35 (equivalent to ~35 goals of weight)
//
// Posterior: Dirichlet(alpha_G + scaled_goals, alpha_A + scaled_assists, alpha_N + scaled_neither)
//   where scaled_goals counts are time-weighted and minutes-scaled.
//
// Output: posterior mean → (prob_score, prob_assist)
//
// Why: Regularizes small-sample estimates — a player with 1 goal in 2 games doesn't get
// an absurdly high probability. The prior shrinks estimates toward position-average,
// which is the formal Bayesian approach to preventing overfitting.
type BayesianPlayerModel struct {
	positionPriors map[int]DirichletPrior
}

// DirichletPrior holds the alpha parameters for the Dirichlet prior.
type DirichletPrior struct {
	AlphaGoal    float64
	AlphaAssist  float64
	AlphaNeither float64
}

const nGoalsPrior = 35.0

// NewBayesianPlayerModel fits empirical Bayes priors from repository data.
func NewBayesianPlayerModel(repo *database.Repository) (*BayesianPlayerModel, error) {
	bpm := &BayesianPlayerModel{
		positionPriors: make(map[int]DirichletPrior),
	}

	for pos := 1; pos <= 4; pos++ {
		prior := bpm.computePriorFromData(repo, pos)
		bpm.positionPriors[pos] = prior
	}

	return bpm, nil
}

// NewBayesianPlayerModelDefault creates a model with reasonable default priors.
// Used when database-driven prior computation is not available.
func NewBayesianPlayerModelDefault() *BayesianPlayerModel {
	return &BayesianPlayerModel{
		positionPriors: map[int]DirichletPrior{
			1: {AlphaGoal: 0.01, AlphaAssist: 0.01, AlphaNeither: nGoalsPrior - 0.02},  // GK
			2: {AlphaGoal: 1.0, AlphaAssist: 1.5, AlphaNeither: nGoalsPrior - 2.5},      // DEF
			3: {AlphaGoal: 2.5, AlphaAssist: 3.0, AlphaNeither: nGoalsPrior - 5.5},      // MID
			4: {AlphaGoal: 4.0, AlphaAssist: 2.0, AlphaNeither: nGoalsPrior - 6.0},      // FWD
		},
	}
}

func (bpm *BayesianPlayerModel) computePriorFromData(repo *database.Repository, position int) DirichletPrior {
	summaries, err := repo.GetPositionStats(30)
	if err != nil || len(summaries) == 0 {
		return defaultPriorForPosition(position)
	}

	for _, s := range summaries {
		if s.Position != position {
			continue
		}
		totalInvolvements := float64(s.TotalGoals + s.TotalAssists)
		if totalInvolvements == 0 {
			return defaultPriorForPosition(position)
		}

		// Scale to prior strength
		alphaG := nGoalsPrior * (float64(s.TotalGoals) / totalInvolvements)
		alphaA := nGoalsPrior * (float64(s.TotalAssists) / totalInvolvements)
		alphaN := nGoalsPrior

		return DirichletPrior{
			AlphaGoal:    alphaG,
			AlphaAssist:  alphaA,
			AlphaNeither: alphaN,
		}
	}

	return defaultPriorForPosition(position)
}

func defaultPriorForPosition(position int) DirichletPrior {
	switch position {
	case 1:
		return DirichletPrior{AlphaGoal: 0.01, AlphaAssist: 0.01, AlphaNeither: nGoalsPrior}
	case 2:
		return DirichletPrior{AlphaGoal: 1.0, AlphaAssist: 1.5, AlphaNeither: nGoalsPrior}
	case 3:
		return DirichletPrior{AlphaGoal: 2.5, AlphaAssist: 3.0, AlphaNeither: nGoalsPrior}
	case 4:
		return DirichletPrior{AlphaGoal: 4.0, AlphaAssist: 2.0, AlphaNeither: nGoalsPrior}
	default:
		return DirichletPrior{AlphaGoal: 2.0, AlphaAssist: 2.0, AlphaNeither: nGoalsPrior}
	}
}

// PosteriorProbabilities computes the posterior mean probabilities for a player.
//
//	posterior_alpha = prior_alpha + scaled_goals_count
//	prob_score = posterior_alpha_G / (posterior_alpha_G + posterior_alpha_A + posterior_alpha_N)
//
// scaled_goals = actual_goals * (time_decay_factor) * (team_minutes / player_minutes)
// This ensures that a 15-minute sub appearance with 1 goal doesn't get the same weight
// as a 90-minute starter with 1 goal.
func (bpm *BayesianPlayerModel) PosteriorProbabilities(
	position int,
	goals int,
	assists int,
	playerMinutes int,
	teamGoals int,
	timeDecayFactor float64,
) (probScore, probAssist float64) {
	prior, ok := bpm.positionPriors[position]
	if !ok {
		prior = defaultPriorForPosition(position)
	}

	scaledGoals := 0.0
	scaledAssists := 0.0
	if playerMinutes > 0 && teamGoals > 0 {
		minsRatio := float64(playerMinutes) / float64(teamGoals) // approximate
		if minsRatio > 0 {
			scaledGoals = float64(goals) * timeDecayFactor * minsRatio
			scaledAssists = float64(assists) * timeDecayFactor * minsRatio
		}
	} else {
		scaledGoals = float64(goals) * timeDecayFactor
		scaledAssists = float64(assists) * timeDecayFactor
	}

	postG := prior.AlphaGoal + scaledGoals
	postA := prior.AlphaAssist + scaledAssists
	postN := prior.AlphaNeither

	total := postG + postA + postN
	if total <= 0 {
		return 0, 0
	}

	probScore = postG / total
	probAssist = postA / total

	return probScore, probAssist
}

// ApplyBayesianModel scores all players with Bayesian posterior probabilities.
// Returns updated PlayerScore slices with ProbScore and ProbAssist populated.
func (bpm *BayesianPlayerModel) ApplyBayesianModel(
	players []models.PlayerScore,
	repo *database.Repository,
) {
	for i := range players {
		stats, err := repo.GetPlayerWeightedRecentStats(players[i].PlayerID, 10)
		if err != nil || stats == nil || stats.Games == 0 {
			continue
		}

		teamGoals := stats.TeamGoals
		if teamGoals <= 0 {
			teamGoals = stats.Goals + stats.Assists
			if teamGoals <= 0 {
				teamGoals = 1
			}
		}

		players[i].ProbScore, players[i].ProbAssist = bpm.PosteriorProbabilities(
			players[i].Position,
			stats.Goals,
			stats.Assists,
			stats.Minutes,
			teamGoals,
			1.0,
		)

		players[i].PriorAlphaScore = bpm.positionPriors[players[i].Position].AlphaGoal
		players[i].PriorAlphaAssist = bpm.positionPriors[players[i].Position].AlphaAssist
		players[i].PriorAlphaNeither = bpm.positionPriors[players[i].Position].AlphaNeither
	}
}

// GetPrior returns the empirical Bayes prior for a given position.
func (bpm *BayesianPlayerModel) GetPrior(position int) DirichletPrior {
	if prior, ok := bpm.positionPriors[position]; ok {
		return prior
	}
	return defaultPriorForPosition(position)
}
