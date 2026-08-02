package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"fpl-core/internal/database"
	"fpl-core/internal/fetcher"
	"fpl-core/internal/models"

	"github.com/robfig/cron/v3"
)

// Config holds application configuration
type Config struct {
	DBPath        string
	FetchSchedule string
	UserID        string
	FPLTeamID     string
}

var (
	runOnce   bool
	runFetch  bool
	seedPrior bool
)

func main() {
	flag.BoolVar(&runOnce, "once", false, "Run once and exit")
	flag.BoolVar(&runFetch, "fetch", false, "Run fetch (with -once)")
	flag.BoolVar(&seedPrior, "seed-prior", false, "Seed vaastav historical data then exit")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("FPL Core Service starting (fetch-only)...")

	// Load configuration
	cfg := loadConfig()

	// Initialize database
	db, err := database.New(database.Config{
		Path: cfg.DBPath,
	})
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	log.Println("Database initialized successfully")

	// Initialize repository
	repo := database.NewRepository(db)

	// Initialize fetcher
	httpClient := fetcher.NewClient(fetcher.Config{
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
		Timeout:    30 * time.Second,
	})
	fetcherSvc := fetcher.NewService(httpClient, repo)

	// Seed prior data from vaastav
	if seedPrior {
		log.Println("Seeding vaastav historical data...")
		vf := fetcher.NewVaastavFetcher(fetcher.GetEstimatedLatestSeason())
		entries, err := vf.FetchAllSeasons()
		if err != nil {
			log.Printf("Warning: vaastav fetch failed: %v", err)
		} else if len(entries) > 0 {
			log.Printf("Got %d entries, storing...", len(entries))
			if err := repo.StoreSeasonHistoryBatch(convertEntries(entries)); err != nil {
				log.Printf("Warning: storing vaastav data failed: %v", err)
			} else {
				for _, season := range fetcher.GetEstimatedLatestSeason() {
					if err := repo.BuildCrossSeasonMap(season); err != nil {
						log.Printf("Warning: cross-season map for %s: %v", season, err)
					}
				}
				repo.SetLastFetch()
				log.Println("Vaastav seed completed")
			}
		}
		log.Println("Vaastav seed done, exiting")
		return
	}

	// Run once mode
	if runOnce {
		log.Println("Running fetch task...")
		if err := fetcherSvc.FetchAllWithTeam(cfg.UserID, cfg.FPLTeamID); err != nil {
			log.Fatalf("Fetch task failed: %v", err)
		}
		log.Println("Fetch completed successfully")
		return
	}

	// Cron mode — fetch only
	c := cron.New()
	var fetchMu sync.Mutex

	runFetch := func() {
		if !fetchMu.TryLock() {
			log.Println("Fetch already in progress, skipping")
			return
		}
		defer fetchMu.Unlock()
		log.Println("Starting fetch task...")
		if err := fetcherSvc.FetchAllWithTeam(cfg.UserID, cfg.FPLTeamID); err != nil {
			log.Printf("Fetch task failed: %v", err)
		} else {
			log.Println("Fetch task completed")
			repo.SetLastFetch()
		}
	}

	_, err = c.AddFunc(cfg.FetchSchedule, runFetch)
	if err != nil {
		log.Fatalf("Failed to schedule fetch task: %v", err)
	}

	c.Start()
	log.Printf("Cron scheduler started - fetch: %s", cfg.FetchSchedule)

	// HTTP trigger endpoint
	http.HandleFunc("/fetch", func(w http.ResponseWriter, r *http.Request) {
		log.Println("HTTP trigger: /fetch requested")
		go runFetch()
		fmt.Fprintln(w, "Fetch triggered")
	})

	go func() {
		log.Println("HTTP endpoint listening on :8090")
		if err := http.ListenAndServe(":8090", nil); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	// Run initial fetch on startup
	go func() {
		time.Sleep(2 * time.Second)
		log.Println("Running initial fetch...")
		runFetch()
	}()

	// Wait for interrupt
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down gracefully...")
	c.Stop()
}

// loadConfig loads configuration from environment variables
func loadConfig() Config {
	return Config{
		DBPath:        getEnv("DB_PATH", "/data/fpl.db"),
		FetchSchedule: getEnv("FETCH_SCHEDULE", "0 */1 * * *"),
		UserID:        getEnv("USER_ID", "default"),
		FPLTeamID:     getEnv("FPL_TEAM_ID", ""),
	}
}

// convertEntries converts vaastav fetcher entries to model entries
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

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
