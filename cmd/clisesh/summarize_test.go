package main

import (
	"testing"

	"github.com/semigodking/clisesh/internal/config"
)

func TestBuildSummarizerResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Summarizer.Subprocess["custom"] = config.SubprocessConfig{
		Command: "echo",
		Args:    []string{"custom", "{prompt}"},
	}
	cfg.Summarizer.LLM["deepseek"] = config.LLMConfig{
		BaseURL:     "https://api.deepseek.com/v1",
		APIKey:      "secret-key",
		Model:       "deepseek-chat",
		Temperature: 0.1,
		Format:      "openai",
		Headers:     map[string]string{"X-Test": "1"},
	}

	tests := []struct {
		backend string
		wantErr bool
	}{
		{"subprocess.kiro", false},
		{"summarizer.subprocess.kiro", false},
		{"subprocess.custom", false},
		{"custom", false},
		{"llm.deepseek", false},
		{"summarizer.llm.deepseek", false},
		{"deepseek", false},
		{"subprocess.nonexistent", true},
		{"llm.nonexistent", true},
		{"nonexistent", true},
	}

	for _, tc := range tests {
		cfg.Summarizer.Backend = tc.backend
		summ, err := buildSummarizer(cfg)
		if (err != nil) != tc.wantErr {
			t.Errorf("backend %q: err = %v, wantErr = %v", tc.backend, err, tc.wantErr)
		}
		if !tc.wantErr && summ == nil {
			t.Errorf("backend %q: expected non-nil summarizer", tc.backend)
		}
	}
}

func TestBuildSummarizerTimeoutAndModelOverrides(t *testing.T) {
	cfg := config.Default()
	cfg.Summarizer.Model = "global-model"
	cfg.Summarizer.TimeoutSec = 30
	cfg.Summarizer.Subprocess["custom"] = config.SubprocessConfig{
		Command:    "agy",
		Model:      "agy-model",
		TimeoutSec: 10,
	}
	cfg.Summarizer.Backend = "subprocess.custom"

	summ, err := buildSummarizer(cfg)
	if err != nil {
		t.Fatalf("buildSummarizer: %v", err)
	}
	if summ == nil {
		t.Fatal("expected non-nil summarizer")
	}
}
