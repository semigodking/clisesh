package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/semigodking/clisesh/internal/summarize"
)

func TestSummarizeRequestAndResponseOpenAIFormat(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"1: HTTP generated summary"}}]}`)
	}))
	defer srv.Close()

	b := New(srv.URL, "sk-secret", "gpt-test", 0.2, "openai", nil, 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"please summarize"}}}
	got, err := b.Summarize(context.Background(), items)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got["id1"] != "HTTP generated summary" {
		t.Errorf("id1 = %q", got["id1"])
	}
	if gotAuth != "Bearer sk-secret" {
		t.Errorf("auth header = %q", gotAuth)
	}
	// Verify request shape.
	var req chatRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if req.Model != "gpt-test" {
		t.Errorf("model = %v", req.Model)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Errorf("unexpected messages: %+v", req.Messages)
	}
}

func TestSummarizeSystemUserFormatAndCustomHeaders(t *testing.T) {
	var gotBody string
	var gotCustomHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCustomHeader = r.Header.Get("X-Custom-Auth")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"1: GLM summary"}}]}`)
	}))
	defer srv.Close()

	headers := map[string]string{
		"X-Custom-Auth": "custom-val",
	}
	b := New(srv.URL, "sk-secret", "glm-4", 0.2, "system_user", headers, 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"test prompt"}}}
	got, err := b.Summarize(context.Background(), items)
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if got["id1"] != "GLM summary" {
		t.Errorf("id1 = %q", got["id1"])
	}
	if gotCustomHeader != "custom-val" {
		t.Errorf("custom header = %q, want custom-val", gotCustomHeader)
	}

	var req chatRequest
	if err := json.Unmarshal([]byte(gotBody), &req); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages for system_user, got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" {
		t.Errorf("messages[0].Role = %q, want system", req.Messages[0].Role)
	}
	if req.Messages[1].Role != "user" {
		t.Errorf("messages[1].Role = %q, want user", req.Messages[1].Role)
	}
}

func TestHTTPErrorRedactsKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo the key back in the error body to test redaction.
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "invalid key sk-secret")
	}))
	defer srv.Close()

	b := New(srv.URL, "sk-secret", "m", 0.2, "openai", nil, 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"p"}}}
	_, err := b.Summarize(context.Background(), items)
	if err == nil {
		t.Fatal("expected error on HTTP 401")
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Errorf("error must redact API key, got: %v", err)
	}
	if !strings.Contains(err.Error(), "***") {
		t.Errorf("error should contain redaction marker, got: %v", err)
	}
}

func TestNoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		io.WriteString(w, `{"choices":[{"message":{"content":"1: ok"}}]}`)
	}))
	defer srv.Close()

	b := New(srv.URL, "", "m", 0.2, "openai", nil, 80, 5*time.Second)
	items := []summarize.Item{{ID: "id1", Prompts: []string{"p"}}}
	if _, err := b.Summarize(context.Background(), items); err != nil {
		t.Fatal(err)
	}
	if hadAuth {
		t.Error("no Authorization header should be sent when API key is empty")
	}
}

func TestTitleShortCircuitsNoHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("HTTP should not be called when titles suffice")
	}))
	defer srv.Close()

	b := New(srv.URL, "", "m", 0.2, "openai", nil, 80, time.Second)
	items := []summarize.Item{{ID: "a", Title: "Good title", Prompts: []string{"has content"}}}
	got, err := b.Summarize(context.Background(), items)
	if err != nil {
		t.Fatal(err)
	}
	if got["a"] != "Good title" {
		t.Errorf("a = %q", got["a"])
	}
}

func TestSummarizeObservesRPMLimiter(t *testing.T) {
	reqCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		resp := chatResponse{
			Choices: []struct {
				Message chatMessage `json:"message"`
			}{
				{Message: chatMessage{Role: "assistant", Content: "1: summary"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	b := NewWithOptions(srv.URL, "key", "m", 0.2, "openai", nil, 80, time.Second, 1, 1500, 2)
	if b.Limiter == nil || b.RPM != 2 {
		t.Fatalf("expected Limiter to be initialized with RPM 2")
	}

	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	currentTime := baseTime
	b.Limiter.now = func() time.Time {
		return currentTime
	}
	sleepCalled := 0
	b.Limiter.sleep = func(ctx context.Context, d time.Duration) error {
		sleepCalled++
		currentTime = currentTime.Add(d)
		return nil
	}

	ctx := context.Background()
	// Request 1: should not sleep
	_, err := b.Summarize(ctx, []summarize.Item{{ID: "1", Prompts: []string{"p1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if sleepCalled != 0 {
		t.Errorf("expected 0 sleeps after req 1, got %d", sleepCalled)
	}

	// Request 2 at T+10s: should not sleep
	currentTime = baseTime.Add(10 * time.Second)
	_, err = b.Summarize(ctx, []summarize.Item{{ID: "2", Prompts: []string{"p2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if sleepCalled != 0 {
		t.Errorf("expected 0 sleeps after req 2, got %d", sleepCalled)
	}

	// Request 3 at T+15s: RPM limit of 2 reached, should sleep until T+60s!
	currentTime = baseTime.Add(15 * time.Second)
	_, err = b.Summarize(ctx, []summarize.Item{{ID: "3", Prompts: []string{"p3"}}})
	if err != nil {
		t.Fatal(err)
	}
	if sleepCalled != 1 {
		t.Errorf("expected 1 sleep after req 3, got %d", sleepCalled)
	}
	if reqCount != 3 {
		t.Errorf("expected 3 HTTP requests, got %d", reqCount)
	}
}

