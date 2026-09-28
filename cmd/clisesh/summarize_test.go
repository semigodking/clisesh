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

func TestBuildSummarizerRPMResolution(t *testing.T) {
	cfg := config.Default()
	cfg.Summarizer.RPM = 30
	cfg.Summarizer.LLM["global-rpm"] = config.LLMConfig{
		BaseURL: "https://api.test.com",
		APIKey:  "key",
		Model:   "m",
	}
	cfg.Summarizer.LLM["custom-rpm"] = config.LLMConfig{
		BaseURL: "https://api.test.com",
		APIKey:  "key",
		Model:   "m",
		RPM:     60,
	}

	// 1. Inherit from global summarizer.rpm
	cfg.Summarizer.Backend = "llm.global-rpm"
	s1, err := buildSummarizer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	llm1, ok := s1.(interface{ GetRPM() int })
	if !ok || llm1.GetRPM() != 30 {
		t.Errorf("expected s1 to have RPM 30, got %v", llm1)
	}

	// 2. Custom per-backend override
	cfg.Summarizer.Backend = "llm.custom-rpm"
	s2, err := buildSummarizer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	llm2, ok := s2.(interface{ GetRPM() int })
	if !ok || llm2.GetRPM() != 60 {
		t.Errorf("expected s2 to have RPM 60, got %v", llm2)
	}
}

