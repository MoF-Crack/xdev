package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/xmdemt/xdev/internal/ai"
)

func newAiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "AI-assisted development environment management",
		Long: `AI-assisted features powered by Ollama.

These commands use a local Ollama instance to provide intelligent
version suggestions, environment diagnostics and more.

Prerequisites:
  - Install Ollama: https://ollama.com
  - Start Ollama: ollama serve
  - Pull a model: ollama pull codellama

All AI features gracefully degrade when Ollama is not available.`,
	}

	cmd.AddCommand(
		newAiSuggestCmd(),
		newAiDiagnoseCmd(),
		newAiStatusCmd(),
		newAiConfigCmd(),
	)

	return cmd
}

// newAiSuggestCmd implements `xdev ai suggest <tool>`.
func newAiSuggestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "suggest <tool>",
		Short: "AI-recommended tool versions",
		Long: `Ask the AI for version recommendations for a specific tool.

The AI considers the currently installed version and suggests
appropriate upgrades or alternatives.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tool := args[0]
			cfg := Cfg()
			assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})

			if !assistant.IsAvailable() {
				warnOllamaUnavailable()
				return nil
			}

			// Find current version
			currentVersion := findCurrentVersion(tool)
			if currentVersion == "" {
				currentVersion = "not installed"
			}
			fmt.Printf("Tool: %s (current: %s)\n", tool, currentVersion)
			fmt.Println("Asking AI for version suggestions...")

			versions, err := assistant.SuggestVersions(tool, currentVersion)
			if err != nil {
				fmt.Printf("⚠ AI suggestion failed: %v\n", err)
				return nil
			}

			if len(versions) == 0 {
				fmt.Println("No suggestions returned by AI")
				return nil
			}

			fmt.Println("\nAI-suggested versions:")
			for i, v := range versions {
				fmt.Printf("  %d. %s\n", i+1, v)
			}
			fmt.Printf("\nInstall with: xdev install %s@<version>\n", tool)
			return nil
		},
	}
}

// newAiDiagnoseCmd implements `xdev ai diagnose`.
func newAiDiagnoseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diagnose",
		Short: "AI-diagnose environment problems",
		Long: `Run an AI-assisted diagnosis of your development environment.

The AI analyzes your xdev installation, PATH configuration and
installed tools to identify and suggest fixes for common issues.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})

			if !assistant.IsAvailable() {
				warnOllamaUnavailable()
				return nil
			}

			// Collect environment info
			var problems []string

			// Check home directory
			if fi, err := os.Stat(cfg.Core.XMDevDir); err != nil || !fi.IsDir() {
				problems = append(problems, "xdev home directory does not exist or is not accessible: "+cfg.Core.XMDevDir)
			}

			// Check PATH
			pathStr := os.Getenv("PATH")
			shimDir := cfg.Core.ShimDir
			if !strings.Contains(strings.ToLower(pathStr), strings.ToLower(shimDir)) {
				problems = append(problems, "shim directory is not in PATH: "+shimDir)
			}

			// Check installed tools
			currentDir := filepath.Join(cfg.Core.XMDevDir, "current")
			if entries, err := os.ReadDir(currentDir); err != nil {
				problems = append(problems, "no tools currently activated")
			} else if len(entries) == 0 {
				problems = append(problems, "no tools currently activated")
			}

			// Check OS/arch
			problems = append(problems, fmt.Sprintf("platform: %s/%s", runtime.GOOS, runtime.GOARCH))

			if len(problems) == 0 {
				fmt.Println("No issues detected. Your environment looks healthy! 🎉")
				return nil
			}

			problemDesc := strings.Join(problems, "\n- ")
			fmt.Println("Analyzing environment with AI...")
			suggestion, err := assistant.DiagnoseIssue(problemDesc)
			if err != nil {
				fmt.Printf("⚠ AI diagnosis failed: %v\n", err)
				return nil
			}

			fmt.Printf("\nAI Diagnosis:\n%s\n", suggestion)
			return nil
		},
	}
}

// newAiStatusCmd implements `xdev ai status`.
func newAiStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check AI service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := Cfg()
			assistant := ai.NewAssistant(&ai.AIConfig{OllamaURL: cfg.AI.OllamaURL, Model: cfg.AI.Model, Timeout: cfg.AI.Timeout})

			fmt.Println("Checking Ollama status...")
			if assistant.IsAvailable() {
				fmt.Println("✔ Ollama is running")
				fmt.Printf("  Endpoint: %s\n", assistant.GetURL())
				fmt.Printf("  Model:    %s\n", assistant.GetModel())
			} else {
				fmt.Println("✘ Ollama is not running")
				fmt.Println()
				fmt.Println("To get started with AI features:")
				fmt.Println("  1. Install Ollama: https://ollama.com")
				fmt.Println("  2. Start Ollama:   ollama serve")
				fmt.Printf("  3. Pull a model:   ollama pull %s\n", assistant.GetModel())
			}
			return nil
		},
	}
}

// findCurrentVersion returns the currently active version of a tool,
// or "" if not installed.
func findCurrentVersion(tool string) string {
	ver, _ := activeVersion(tool)
	return ver
}

// newAiConfigCmd implements `xdev ai config`.
func newAiConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or set AI configuration",
	}
	var url, model, timeout string
	cmd.Flags().StringVar(&url, "url", "", "Set Ollama API URL")
	cmd.Flags().StringVar(&model, "model", "", "Set AI model name")
	cmd.Flags().StringVar(&timeout, "timeout", "", "Set timeout (e.g. 60s, 2m)")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		cfg := Cfg()
		changed := false
		if cmd.Flags().Changed("url") {
			cfg.AI.OllamaURL = url
			changed = true
		}
		if cmd.Flags().Changed("model") {
			cfg.AI.Model = model
			changed = true
		}
		if cmd.Flags().Changed("timeout") {
			cfg.AI.Timeout = timeout
			changed = true
		}
		if changed {
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Println("✔ AI configuration updated")
		}
		fmt.Printf("AI Configuration:\n")
		fmt.Printf("  Ollama URL: %s\n", cfg.AI.OllamaURL)
		fmt.Printf("  Model:      %s\n", cfg.AI.Model)
		fmt.Printf("  Timeout:    %s\n", cfg.AI.Timeout)
		return nil
	}
	return cmd
}
