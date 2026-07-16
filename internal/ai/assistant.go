// Package ai provides AI-assisted features via Ollama.
//
// This package uses the Ollama HTTP API (http://localhost:11434)
// to provide version suggestions and environment diagnostics.
// All features gracefully degrade when Ollama is not available.
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AIConfig holds AI assistant settings.
type AIConfig struct {
	OllamaURL string
	Model     string
	Timeout   string
}

// Assistant wraps the Ollama API for AI-assisted features.
type Assistant struct {
	ollamaURL string // default: http://localhost:11434
	model     string // default: codellama
	client    *http.Client
}

// NewAssistant creates a new AI assistant. If cfg is non-nil, its
// AI settings override the defaults.
func NewAssistant(cfg *AIConfig) *Assistant {
	a := &Assistant{
		ollamaURL: "http://localhost:11434",
		model:     "codellama",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	if cfg != nil {
		if cfg.OllamaURL != "" {
			// Security (N-7): restrict Ollama URLs to localhost to
			// prevent AI diagnostic data (which may include environment
			// details) from being sent to an attacker-controlled server.
			if !isLocalOllamaURL(cfg.OllamaURL) {
				log.Printf("[ai] WARNING: Ollama URL %q is not localhost — AI features disabled for security (diagnostic data may include environment details)", cfg.OllamaURL)
				a.ollamaURL = ""
			} else {
				a.ollamaURL = strings.TrimRight(cfg.OllamaURL, "/")
			}
		}
		if cfg.Model != "" {
			a.model = cfg.Model
		}
		if cfg.Timeout != "" {
			if d, err := time.ParseDuration(cfg.Timeout); err == nil {
				a.client.Timeout = d
			}
		}
	}
	return a
}

// SetOllamaURL overrides the default Ollama API endpoint.
// Security (N-7): only localhost URLs are accepted to prevent
// diagnostic data from being sent to remote/attacker-controlled servers.
func (a *Assistant) SetOllamaURL(url string) {
	if !isLocalOllamaURL(url) {
		log.Printf("[ai] WARNING: Ollama URL %q is not localhost — AI features disabled for security", url)
		a.ollamaURL = ""
		return
	}
	a.ollamaURL = strings.TrimRight(url, "/")
}

// isLocalOllamaURL reports whether url points to the local machine.
// Only http(s) to localhost/127.0.0.1/::1 is accepted.
func isLocalOllamaURL(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == ""
}

// SetModel overrides the default model name.
func (a *Assistant) SetModel(model string) {
	a.model = model
}

// GetURL returns the configured Ollama API endpoint URL.
func (a *Assistant) GetURL() string {
	return a.ollamaURL
}

// GetModel returns the configured model name.
func (a *Assistant) GetModel() string {
	return a.model
}

// IsAvailable checks whether Ollama is running and reachable.
func (a *Assistant) IsAvailable() bool {
	resp, err := a.client.Get(a.ollamaURL + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ollamaRequest is the JSON body sent to /api/generate.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaResponse is the JSON body returned by /api/generate.
type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// generate sends a prompt to Ollama and returns the response text.
// Returns an error if Ollama is unavailable or the request fails.
func (a *Assistant) generate(prompt string) (string, error) {
	reqBody := ollamaRequest{
		Model:  a.model,
		Prompt: prompt,
		Stream: false,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := a.client.Post(
		a.ollamaURL+"/api/generate",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return "", fmt.Errorf("ollama request timed out after %s (is Ollama responding?)", a.client.Timeout)
		}
		return "", fmt.Errorf("ollama request failed: %w (is Ollama running?)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode ollama response: %w", err)
	}

	return strings.TrimSpace(result.Response), nil
}

// SuggestVersions asks the AI for version recommendations for a tool.
// It returns a list of suggested version strings.
func (a *Assistant) SuggestVersions(tool string, currentVersion string) ([]string, error) {
	prompt := fmt.Sprintf(
		"You are a development environment advisor. For the tool %q, the user currently has version %s installed. "+
			"List the recommended versions to consider upgrading to, one per line, with no explanation. "+
			"Only output version numbers, nothing else. If the current version is already the best, output it.",
		tool, currentVersion,
	)

	response, err := a.generate(prompt)
	if err != nil {
		return nil, err
	}

	var versions []string
	for _, line := range strings.Split(response, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip common prefixes like "- " or "* " or "• "
		line = strings.TrimLeft(line, "- *•.) ")
		// Strip leading "N. " pattern from numbered lists (e.g., "1. 20.10.0")
		if len(line) > 2 && line[0] >= '0' && line[0] <= '9' {
			dotIdx := strings.Index(line, ".")
			if dotIdx > 0 && dotIdx < 4 && dotIdx+1 < len(line) && (line[dotIdx+1] == ' ' || line[dotIdx+1] == '\t') {
				line = strings.TrimLeft(line[dotIdx+1:], " \t")
			}
		}
		if line != "" {
			versions = append(versions, line)
		}
	}
	return versions, nil
}

// DiagnoseIssue sends a diagnostic problem description to the AI
// and returns a suggested fix or explanation.
func (a *Assistant) DiagnoseIssue(problem string) (string, error) {
	prompt := fmt.Sprintf(
		"You are a development environment diagnostic expert. The user is experiencing the following issue with xdev (a development environment manager):\n\n%s\n\n"+
			"Provide a concise diagnosis and suggested fix. Keep the response under 200 words.",
		problem,
	)

	response, err := a.generate(prompt)
	if err != nil {
		return "", err
	}

	return response, nil
}

// Generate sends a prompt to the AI and returns the response.
// This is a general-purpose single-turn API for the GUI layer.
func (a *Assistant) Generate(prompt string) (string, error) {
	return a.generate(prompt)
}
