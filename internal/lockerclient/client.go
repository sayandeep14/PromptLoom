// Package lockerclient provides a thin HTTP client for the loomlocker server.
package lockerclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client talks to a running loomlocker server.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client targeting the given base URL (e.g. "http://localhost:8053").
func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL + "/api",
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

// IsRunning returns true if the loomlocker server is reachable.
func (c *Client) IsRunning() bool {
	resp, err := c.http.Get(c.baseURL + "/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsLocked returns the current lock state. Returns false if the server is unreachable.
func (c *Client) IsLocked() bool {
	resp, err := c.http.Get(c.baseURL + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Locked bool `json:"locked"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Locked
}

// Unlock sends the password to the server and requests an unlock.
// Returns error if the password is wrong or the server is unreachable.
func (c *Client) Unlock(password string) error {
	body, _ := json.Marshal(map[string]string{"password": password})
	resp, err := c.http.Post(c.baseURL+"/unlock", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("loomlocker unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid password")
	}
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string `json:"error"` }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return fmt.Errorf("unlock failed: %s", e.Error)
	}
	return nil
}

// Lock requests an immediate lock (no password required).
func (c *Client) Lock() error {
	resp, err := c.http.Post(c.baseURL+"/lock", "application/json",
		bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("loomlocker unreachable: %w", err)
	}
	resp.Body.Close()
	return nil
}
