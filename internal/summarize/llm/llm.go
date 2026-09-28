// Package llm implements summarize.Summarizer against an HTTP API (OpenAI,
// DeepSeek, GLM, etc.) supporting customizable request formatting, models,
// temperatures, timeouts, and headers. The API key is never logged and is
// redacted from error messages.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/semigodking/clisesh/internal/summarize"
)

// Format constants determine how the prompt is structured in the HTTP payload.
const (
	FormatOpenAI     = "openai"
	FormatSystemUser = "system_user"
)

// Backend calls an HTTP chat-completions endpoint.
type Backend struct {
	BaseURL     string
	APIKey      string
	Model       string
	Temperature float64
	Timeout     time.Duration
	Format      string
	Headers     map[string]string
	BatchSize   int
	MaxChars    int
	TitleLen    int
	RPM         int
	Limiter     *Limiter
	Client      *http.Client
}

// New returns a configured HTTP LLM backend with defaults.
func New(baseURL, apiKey, model string, temperature float64, format string, headers map[string]string, titleLen int, timeout time.Duration) *Backend {
	return NewWithOptions(baseURL, apiKey, model, temperature, format, headers, titleLen, timeout, summarize.DefaultBatchSize, 1500, 0)
}

// NewWithOptions returns a configured HTTP LLM backend with custom batch size, max chars, and rpm.
func NewWithOptions(baseURL, apiKey, model string, temperature float64, format string, headers map[string]string, titleLen int, timeout time.Duration, batchSize, maxChars, rpm int) *Backend {
	if format == "" {
		format = FormatOpenAI
	}
	if batchSize <= 0 {
		batchSize = summarize.DefaultBatchSize
	}
	if maxChars <= 0 {
		maxChars = 1500
	}
	var lim *Limiter
	if rpm > 0 {
		lim = NewLimiter(rpm)
	}
	return &Backend{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		APIKey:      apiKey,
		Model:       model,
		Temperature: temperature,
		Timeout:     timeout,
		Format:      format,
		Headers:     headers,
		BatchSize:   batchSize,
		MaxChars:    maxChars,
		TitleLen:    titleLen,
		RPM:         rpm,
		Limiter:     lim,
		Client:      &http.Client{},
	}
}

// SetRPM configures or updates the requests per minute rate limiter.
func (b *Backend) SetRPM(rpm int) {
	b.RPM = rpm
	if rpm > 0 {
		b.Limiter = NewLimiter(rpm)
	} else {
		b.Limiter = nil
	}
}

// SetLimiter sets a custom rate limiter on the backend (e.g. for testing).
func (b *Backend) SetLimiter(lim *Limiter) {
	b.Limiter = lim
	if lim != nil {
		b.RPM = lim.RPM()
	} else {
		b.RPM = 0
	}
}

// GetRPM returns the configured requests per minute.
func (b *Backend) GetRPM() int {
	return b.RPM
}

// Summarize implements summarize.Summarizer.
func (b *Backend) Summarize(ctx context.Context, items []summarize.Item) (map[string]string, error) {
	return summarize.RunBatched(ctx, items, b.TitleLen, b.MaxChars, b.BatchSize,
		func(ctx context.Context, prompt string) (string, error) {
			return b.chat(ctx, prompt)
		})
}

type chatRequest struct {
	Model       string        `json:"model"`
	Temperature float64       `json:"temperature"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

func (b *Backend) buildMessages(prompt string) []chatMessage {
	switch b.Format {
	case FormatSystemUser:
		// If system_user, extract header instructions into a system message,
		// and session contents into the user message.
		systemInstruction := fmt.Sprintf("You are a concise session titling assistant. Generate brief, descriptive titles (under %d characters). Focus on the main task. Respond with ONLY a numbered list '<n>: <description>'.", b.TitleLen)
		return []chatMessage{
			{Role: "system", Content: systemInstruction},
			{Role: "user", Content: prompt},
		}
	default:
		// Default standard OpenAI style: single user prompt containing instructions & sessions.
		return []chatMessage{
			{Role: "user", Content: prompt},
		}
	}
}

func (b *Backend) chat(ctx context.Context, prompt string) (string, error) {
	if b.Limiter != nil {
		if err := b.Limiter.Wait(ctx); err != nil {
			return "", err
		}
	}

	timeout := b.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reqBody, err := json.Marshal(chatRequest{
		Model:       b.Model,
		Temperature: b.Temperature,
		Messages:    b.buildMessages(prompt),
	})
	if err != nil {
		return "", err
	}

	url := b.BaseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.APIKey)
	}
	for k, v := range b.Headers {
		req.Header.Set(k, v)
	}

	resp, err := b.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", b.redactErr(err))
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, b.redact(string(body)))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return cr.Choices[0].Message.Content, nil
}

// redact removes the API key from a string (defense in depth for error text).
func (b *Backend) redact(s string) string {
	if b.APIKey == "" {
		return s
	}
	return strings.ReplaceAll(s, b.APIKey, "***")
}

// redactErr wraps redact for error values.
func (b *Backend) redactErr(err error) error {
	if err == nil || b.APIKey == "" {
		return err
	}
	return fmt.Errorf("%s", b.redact(err.Error()))
}
