package ai

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewAssistant(t *testing.T) {
	a := NewAssistant(nil)
	if url := a.GetURL(); url != "http://localhost:11434" {
		t.Errorf("default URL = %q, want %q", url, "http://localhost:11434")
	}
	if model := a.GetModel(); model != "codellama" {
		t.Errorf("default model = %q, want %q", model, "codellama")
	}
}

func TestSetOllamaURL(t *testing.T) {
	a := NewAssistant(nil)
	// Security (N-7): non-localhost URLs must be rejected to prevent
	// diagnostic data from being sent to remote/attacker-controlled servers.
	a.SetOllamaURL("http://custom:9999/")
	if url := a.GetURL(); url != "" {
		t.Errorf("GetURL() = %q, want %q (non-localhost URL must be rejected for security)", url, "")
	}
	a.SetOllamaURL("http://other:1234///")
	if url := a.GetURL(); url != "" {
		t.Errorf("GetURL() = %q, want %q (non-localhost URL must be rejected)", url, "")
	}
	// localhost URLs should be accepted.
	a.SetOllamaURL("http://localhost:11434/")
	if url := a.GetURL(); url != "http://localhost:11434" {
		t.Errorf("GetURL() = %q, want %q (localhost should be accepted, trailing slash trimmed)", url, "http://localhost:11434")
	}
	a.SetOllamaURL("http://127.0.0.1:9999///")
	if url := a.GetURL(); url != "http://127.0.0.1:9999" {
		t.Errorf("GetURL() = %q, want %q (127.0.0.1 should be accepted, trailing slashes trimmed)", url, "http://127.0.0.1:9999")
	}
}

func TestSetModel(t *testing.T) {
	a := NewAssistant(nil)
	a.SetModel("llama3")
	if model := a.GetModel(); model != "llama3" {
		t.Errorf("GetModel() = %q, want %q", model, "llama3")
	}
}

func TestIsAvailable(t *testing.T) {
	a := NewAssistant(nil)
	// Just verify no panic; result depends on whether Ollama is running.
	_ = a.IsAvailable()
}

func newUnavailableAssistant() *Assistant {
	a := NewAssistant(nil)
	a.SetOllamaURL("http://127.0.0.1:1")
	a.client = &http.Client{Timeout: 2 * time.Second}
	return a
}

func TestSuggestVersionsUnavailable(t *testing.T) {
	a := newUnavailableAssistant()
	_, err := a.SuggestVersions("gcc", "12.0")
	if err == nil {
		t.Fatal("SuggestVersions expected error when Ollama is unavailable, got nil")
	}
	errLower := strings.ToLower(err.Error())
	if !strings.Contains(errLower, "ollama") && !strings.Contains(errLower, "failed") {
		t.Errorf("error %q should contain 'ollama' or 'failed'", err.Error())
	}
}

func TestDiagnoseIssueUnavailable(t *testing.T) {
	a := newUnavailableAssistant()
	_, err := a.DiagnoseIssue("build fails")
	if err == nil {
		t.Fatal("DiagnoseIssue expected error when Ollama is unavailable, got nil")
	}
}

func TestGenerateUnavailable(t *testing.T) {
	a := newUnavailableAssistant()
	_, err := a.Generate("hello")
	if err == nil {
		t.Fatal("Generate expected error when Ollama is unavailable, got nil")
	}
}
