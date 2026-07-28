package trader

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"fpl-bot/internal/database"
	"fpl-bot/internal/models"
)

const (
	fplTransferURL = "https://fantasy.premierleague.com/api/my-team/%s/"
	maxRetries     = 3
	retryDelay     = 2 * time.Second
)

// Service handles transfer execution
type Service struct {
	repo          *database.Repository
	sessionCookie string
	teamID        string
	userID        string
}

// NewService creates a new trader service
func NewService(repo *database.Repository, sessionCookie, teamID, userID string) *Service {
	return &Service{
		repo:          repo,
		sessionCookie: sessionCookie,
		teamID:        teamID,
		userID:        userID,
	}
}

// ExecuteTransfer executes a transfer for a recommendation
func (s *Service) ExecuteTransfer(rec *models.Recommendation) models.TransferResult {
	if s.teamID == "" {
		return models.TransferResult{
			Success: false,
			Error:   "FPL team ID not configured",
		}
	}

	// Try fresh cookie from DB first (written by core's auto-login), fall back to static
	cookie := s.sessionCookie
	if dbCookie, err := s.repo.GetMetadata("fpl_session_cookie"); err == nil && dbCookie != "" {
		cookie = dbCookie
	}
	if cookie == "" {
		return models.TransferResult{
			Success: false,
			Error:   "No FPL session cookie available (configure email/password or session cookie)",
		}
	}

	// Get player prices
	sellPrice, err := s.repo.GetPlayerPrice(rec.SellPlayerID)
	if err != nil {
		return models.TransferResult{
			Success: false,
			Error:   fmt.Sprintf("failed to get sell price: %v", err),
		}
	}

	buyPrice, err := s.repo.GetPlayerPrice(rec.BuyPlayerID)
	if err != nil {
		return models.TransferResult{
			Success: false,
			Error:   fmt.Sprintf("failed to get buy price: %v", err),
		}
	}

	// Build transfer request
	transferReq := models.TransferRequest{
		Transfers: []models.Transfer{
			{
				ElementOut:    rec.SellPlayerID,
				ElementIn:     rec.BuyPlayerID,
				SellingPrice:  sellPrice,
				PurchasePrice: buyPrice,
			},
		},
	}

	requestJSON, err := json.Marshal(transferReq)
	if err != nil {
		return models.TransferResult{
			Success: false,
			Error:   fmt.Sprintf("failed to marshal request: %v", err),
		}
	}

	// Execute transfer with retry
	var result models.TransferResult
	transferURL := fmt.Sprintf(fplTransferURL, s.teamID)

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			time.Sleep(retryDelay * time.Duration(attempt))
		}

		resp, err := s.makeTransferRequest(transferURL, requestJSON, cookie)
		if err != nil {
			result.Error = err.Error()
			log.Printf("Transfer attempt %d failed: %v", attempt, err)
			continue
		}

		result.Success = true
		result.Response = resp
		break
	}

	// Log the transfer attempt
	s.logTransfer(rec.ID, string(requestJSON), result)

	return result
}

// makeTransferRequest makes an HTTP request to the FPL API
func (s *Service) makeTransferRequest(url string, requestBody []byte, cookie string) (string, error) {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", fmt.Sprintf("pl_profile=%s", cookie))
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://fantasy.premierleague.com/")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status: %d, body: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

// logTransfer logs a transfer execution to the database
func (s *Service) logTransfer(recID int, request string, result models.TransferResult) {
	status := "success"
	if !result.Success {
		status = "failed"
	}

	err := s.repo.LogTransfer(s.userID, recID, request, result.Response, status, result.Error)
	if err != nil {
		log.Printf("Failed to log transfer: %v", err)
	}
}
