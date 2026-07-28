package main

import (
	"flag"
	"log"
	"os"
	"time"

	"fpl-core/internal/database"
	"fpl-core/internal/fetcher"
	"fpl-core/internal/models"
)

func main() {
	dbPath := flag.String("db", "fpl.db", "Path to SQLite database")
	schemaPath := flag.String("schema", "sql/schema.sql", "Path to schema.sql file")
	flag.Parse()

	log.Println("Seed: opening database...")
	db, err := database.New(database.Config{
		Path:       *dbPath,
		SchemaPath: *schemaPath,
	})
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()
	repo := database.NewRepository(db)

	client := fetcher.NewClient(fetcher.Config{MaxRetries: 3, Timeout: 30 * time.Second})
	fetchSvc := fetcher.NewService(client, repo)

	log.Println("=== Step 1: Fetching FPL bootstrap data (26/27) ===")
	if err := fetchSvc.FetchAll(); err != nil {
		log.Printf("ERROR: Bootstrap fetch failed: %v", err)
		os.Exit(1)
	}
	log.Println("Bootstrap done — players and teams stored")

	seasons := []string{"2024-25", "2025-26"}
	vf := fetcher.NewVaastavFetcher(seasons)

	log.Printf("=== Step 2: Fetching vaastav data for %v ===", seasons)
	entries, err := vf.FetchAllSeasons()
	if err != nil {
		log.Printf("Warning: vaastav fetch errors: %v", err)
	}
	log.Printf("Got %d season history entries from vaastav", len(entries))

	if len(entries) > 0 {
		if err := repo.StoreSeasonHistoryBatch(convertEntries(entries)); err != nil {
			log.Printf("ERROR: Failed to store season history: %v", err)
			os.Exit(1)
		}
		log.Println("Season history stored")

		log.Println("=== Step 3: Building cross-season maps ===")
		for _, season := range seasons {
			if err := repo.BuildCrossSeasonMap(season); err != nil {
				log.Printf("Warning: cross-season map for %s failed: %v", season, err)
			} else {
				log.Printf("Cross-season map built for %s", season)
			}
		}
	}

	repo.SetLastFetch()
	log.Println("=== Seed complete ===")
}

func convertEntries(entries []fetcher.SeasonHistoryEntry) []models.SeasonHistoryEntry {
	result := make([]models.SeasonHistoryEntry, len(entries))
	for i, e := range entries {
		result[i] = models.SeasonHistoryEntry{
			Season:                   e.Season,
			PlayerName:               e.PlayerName,
			Position:                 e.Position,
			TeamName:                 e.TeamName,
			Event:                    e.Event,
			Minutes:                  e.Minutes,
			TotalPoints:              e.TotalPoints,
			GoalsScored:              e.GoalsScored,
			Assists:                  e.Assists,
			CleanSheets:              e.CleanSheets,
			GoalsConceded:            e.GoalsConceded,
			ExpectedGoals:            e.ExpectedGoals,
			ExpectedAssists:          e.ExpectedAssists,
			ExpectedGoalInvolvements: e.ExpectedGoalInvolvements,
			ExpectedGoalsConceded:    e.ExpectedGoalsConceded,
			Bonus:                    e.Bonus,
			BPS:                      e.BPS,
			Influence:                e.Influence,
			Creativity:               e.Creativity,
			Threat:                   e.Threat,
			ICTIndex:                 e.ICTIndex,
			WasHome:                  e.WasHome,
			OpponentTeam:             e.OpponentTeam,
			KickoffTime:              e.KickoffTime,
			Value:                    e.Value,
			Selected:                 e.Selected,
		}
	}
	return result
}
