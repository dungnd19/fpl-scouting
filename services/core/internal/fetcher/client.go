package fetcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client handles HTTP requests to FPL API
type Client struct {
	httpClient *http.Client
	maxRetries int
	retryDelay time.Duration
}

// Config holds client configuration
type Config struct {
	MaxRetries int
	RetryDelay time.Duration
	Timeout    time.Duration
}

// NewClient creates a new FPL API client
func NewClient(cfg Config) *Client {
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 2 * time.Second
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
		maxRetries: cfg.MaxRetries,
		retryDelay: cfg.RetryDelay,
	}
}

// Fetch performs an HTTP GET request with retry logic
func (c *Client) Fetch(url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			delay := c.retryDelay * time.Duration(attempt)
			time.Sleep(delay)
		}

		resp, err := c.httpClient.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if err != nil {
			lastErr = fmt.Errorf("failed to read response: %w", err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("bad status code: %d, body: %s", resp.StatusCode, string(body))
			continue
		}

		return body, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", c.maxRetries, lastErr)
}

// FetchJSON performs a GET request and unmarshals JSON response
func (c *Client) FetchJSON(url string, v interface{}) error {
	data, err := c.Fetch(url)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}
